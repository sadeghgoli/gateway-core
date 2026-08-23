package proxy

import (
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"time"

	"sabzevar.ir/gateway-core/internal/access"
	"sabzevar.ir/gateway-core/internal/models"
	"sabzevar.ir/gateway-core/internal/queue"
	"sabzevar.ir/gateway-core/internal/registry"
	"sabzevar.ir/gateway-core/internal/store"
	"sabzevar.ir/gateway-core/internal/upstream"
)

type Handler struct {
	reg      *registry.Registry
	limiters *queue.Manager
	selector upstream.UpstreamSelector
	store    *store.Store
}

func New(reg *registry.Registry, limiters *queue.Manager, selector upstream.UpstreamSelector, st *store.Store) *Handler {
	return &Handler{reg: reg, limiters: limiters, selector: selector, store: st}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := stripPort(r.Host)
	gw := h.reg.ByHost(host)
	if gw == nil || !gw.Enabled {
		http.Error(w, "unknown or disabled gateway host", http.StatusNotFound)
		return
	}
	if !access.Allowed(*gw, r) {
		h.store.IncrRejected(gw.ID)
		http.Error(w, "forbidden: this client is not allowed to use the gateway", http.StatusForbidden)
		return
	}
	route := matchRoute(gw.Routes, r.URL.Path)
	corsOrigin := access.CORSOrigin(*gw, r)
	release, err := h.limiters.For(*gw).Acquire(r.Context())
	if err != nil {
		h.store.IncrRejected(gw.ID)
		if err == queue.ErrQueueFull {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		http.Error(w, "gateway busy", http.StatusServiceUnavailable)
		return
	}
	defer release()

	up, err := h.selector.Pick(r.Context(), *gw)
	if err != nil {
		h.store.IncrErrors(gw.ID)
		http.Error(w, "no upstream", http.StatusBadGateway)
		return
	}
	target, err := url.Parse(up.EffectiveURL())
	if err != nil || target.Scheme == "" || target.Host == "" {
		h.selector.Report(up.ID, 0, err)
		h.store.IncrErrors(gw.ID)
		http.Error(w, "invalid upstream", http.StatusBadGateway)
		return
	}

	if r.Method == http.MethodOptions && corsOrigin != "" {
		setCORS(w, corsOrigin, r)
		w.WriteHeader(http.StatusNoContent)
		h.store.IncrCount(gw.ID)
		h.selector.Report(up.ID, 0, nil)
		return
	}

	origHost := host
	origURL := *r.URL
	start := time.Now()
	proxyErr := error(nil)
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1
	rp.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, e error) {
		proxyErr = e
		if !gw.Sensitive {
			log.Printf("proxy error host=%s upstream=%s err=%v", origHost, target.Host, e)
		} else {
			log.Printf("proxy error gateway=%s", gw.ID)
		}
		http.Error(rw, "upstream error", http.StatusBadGateway)
	}
	rp.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host
		applyRoute(req, route)
		access.StripCredentialHeaders(req, *gw)
		req.Header.Set("X-Forwarded-Host", origHost)
		req.Header.Set("X-Forwarded-Proto", forwardedProto(r))
		req.Header.Set("X-Forwarded-Gateway", gw.Host)
		if clientIP := clientIP(r); clientIP != "" {
			req.Header.Set("X-Real-IP", clientIP)
		}
	}
	rp.ModifyResponse = func(resp *http.Response) error {
		rewriteLocation(resp, target, origHost, forwardedProto(r))
		rewriteCookies(resp, target.Hostname(), origHost)
		if corsOrigin != "" {
			resp.Header.Set("Access-Control-Allow-Origin", corsOrigin)
			resp.Header.Set("Access-Control-Allow-Credentials", "true")
		}
		return nil
	}
	rp.ServeHTTP(w, r)
	h.selector.Report(up.ID, time.Since(start), proxyErr)
	h.store.IncrCount(gw.ID)
	if proxyErr != nil {
		h.store.IncrErrors(gw.ID)
	}
	_ = origURL
}

func matchRoute(routes []models.Route, path string) *models.Route {
	var fallback *models.Route
	for i := range routes {
		rt := &routes[i]
		if rt.PathRegex != "" {
			re, err := regexp.Compile(rt.PathRegex)
			if err == nil && re.MatchString(path) {
				return rt
			}
			continue
		}
		prefix := rt.PathPrefix
		if prefix == "" {
			prefix = "/"
		}
		if prefix == "/" {
			if fallback == nil {
				fallback = rt
			}
			continue
		}
		if strings.HasPrefix(path, prefix) {
			return rt
		}
	}
	return fallback
}

func applyRoute(req *http.Request, route *models.Route) {
	if route == nil {
		return
	}
	path := req.URL.Path
	if route.StripPrefix != "" {
		path = strings.TrimPrefix(path, route.StripPrefix)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	}
	if route.AddPrefix != "" {
		path = strings.TrimRight(route.AddPrefix, "/") + path
	}
	req.URL.Path = path
	q := req.URL.Query()
	for _, k := range route.RemoveQuery {
		q.Del(k)
	}
	for k, v := range route.SetQuery {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	for k, v := range route.SetHeaders {
		req.Header.Set(k, v)
	}
}

func rewriteLocation(resp *http.Response, target *url.URL, pubHost, proto string) {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return
	}
	u, err := url.Parse(loc)
	if err != nil {
		return
	}
	if u.Host == "" || strings.EqualFold(u.Hostname(), target.Hostname()) {
		u.Scheme = proto
		u.Host = pubHost
		resp.Header.Set("Location", u.String())
	}
}

func rewriteCookies(resp *http.Response, backendHost, pubHost string) {
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return
	}
	resp.Header.Del("Set-Cookie")
	for _, c := range cookies {
		if c.Domain != "" && (strings.EqualFold(c.Domain, backendHost) || strings.EqualFold(strings.TrimPrefix(c.Domain, "."), backendHost)) {
			c.Domain = pubHost
		}
		if v := c.String(); v != "" {
			resp.Header.Add("Set-Cookie", v)
		}
	}
}

func setCORS(w http.ResponseWriter, origin string, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
	if w.Header().Get("Access-Control-Allow-Headers") == "" {
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Gateway-Token, X-Api-Key")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
}

func stripPort(host string) string {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(h)
}

func forwardedProto(r *http.Request) string {
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		return p
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
