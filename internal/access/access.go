package access

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"

	"sabzevar.ir/gateway-core/internal/models"
)

func Restricted(gw models.Gateway) bool {
	return len(normalizeList(gw.AllowedOrigins)) > 0 || hasEnabledToken(gw.AccessTokens)
}

func Allowed(gw models.Gateway, r *http.Request) bool {
	if !Restricted(gw) {
		return true
	}
	if tokenOK(ExtractToken(r), gw.AccessTokens) {
		return true
	}
	origins := normalizeList(gw.AllowedOrigins)
	if len(origins) == 0 {
		return false
	}
	return originOK(callerOrigin(r), origins)
}

func ExtractToken(r *http.Request) string {
	if t := strings.TrimSpace(r.Header.Get("X-Gateway-Token")); t != "" {
		return t
	}
	if t := strings.TrimSpace(r.Header.Get("X-Api-Key")); t != "" {
		return t
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	q := r.URL.Query()
	if t := strings.TrimSpace(q.Get("gateway_token")); t != "" {
		return t
	}
	return strings.TrimSpace(q.Get("api_key"))
}

func TokenMatches(gw models.Gateway, token string) bool {
	return tokenOK(token, gw.AccessTokens)
}

func OriginAllowed(allowed []string, origin string) bool {
	return originOK(origin, normalizeList(allowed))
}

func CORSOrigin(gw models.Gateway, r *http.Request) string {
	reqOrigin := strings.TrimSpace(r.Header.Get("Origin"))
	if reqOrigin != "" && OriginAllowed(gw.AllowedOrigins, reqOrigin) {
		return reqOrigin
	}
	return gw.CORSAllowOrigin
}

func StripCredentialHeaders(req *http.Request, gw models.Gateway) {
	tok := ExtractToken(req)
	if TokenMatches(gw, tok) {
		auth := req.Header.Get("Authorization")
		if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") && strings.TrimSpace(auth[7:]) == tok {
			req.Header.Del("Authorization")
		}
	}
	req.Header.Del("X-Gateway-Token")
	req.Header.Del("X-Api-Key")
	q := req.URL.Query()
	q.Del("gateway_token")
	q.Del("api_key")
	req.URL.RawQuery = q.Encode()
}

func hasEnabledToken(tokens []models.AccessToken) bool {
	for _, t := range tokens {
		if t.Enabled && strings.TrimSpace(t.Token) != "" {
			return true
		}
	}
	return false
}

func tokenOK(got string, tokens []models.AccessToken) bool {
	if got == "" {
		return false
	}
	ok := false
	gb := []byte(got)
	for _, t := range tokens {
		if !t.Enabled {
			continue
		}
		want := strings.TrimSpace(t.Token)
		if want == "" {
			continue
		}
		wb := []byte(want)
		if len(wb) != len(gb) {
			continue
		}
		if subtle.ConstantTimeCompare(gb, wb) == 1 {
			ok = true
		}
	}
	return ok
}

func callerOrigin(r *http.Request) string {
	if o := strings.TrimSpace(r.Header.Get("Origin")); o != "" && o != "null" {
		return o
	}
	return strings.TrimSpace(r.Header.Get("Referer"))
}

func originOK(raw string, allowed []string) bool {
	host := callerHost(raw)
	if host == "" {
		return false
	}
	for _, a := range allowed {
		if matchHost(host, a) {
			return true
		}
	}
	return false
}

func callerHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "null") {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	return strings.TrimPrefix(host, ".")
}

func matchHost(host, pattern string) bool {
	p := strings.ToLower(strings.TrimSpace(pattern))
	p = strings.TrimPrefix(p, ".")
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "*.") {
		suf := p[1:] // .example.com
		return strings.HasSuffix(host, suf) && host != strings.TrimPrefix(suf, ".")
	}
	return host == p
}

func normalizeList(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		for _, part := range strings.Split(item, ",") {
			p := strings.ToLower(strings.TrimSpace(part))
			p = strings.TrimSuffix(p, "/")
			if p == "" {
				continue
			}
			if strings.Contains(p, "://") {
				if u, err := url.Parse(p); err == nil && u.Hostname() != "" {
					p = strings.ToLower(u.Hostname())
					if strings.HasPrefix(strings.TrimSpace(part), "*.") || strings.HasPrefix(u.Hostname(), "*.") {
						p = "*." + strings.TrimPrefix(p, "*.")
					}
				}
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}
