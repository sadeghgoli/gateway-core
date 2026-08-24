package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"sabzevar.ir/gateway-core/internal/config"
	"sabzevar.ir/gateway-core/internal/models"
	"sabzevar.ir/gateway-core/internal/nginxctl"
	"sabzevar.ir/gateway-core/internal/queue"
	"sabzevar.ir/gateway-core/internal/registry"
	"sabzevar.ir/gateway-core/internal/store"
	"sabzevar.ir/gateway-core/internal/upstream"
)

type Server struct {
	cfg      config.Config
	store    *store.Store
	reg      *registry.Registry
	limiters *queue.Manager
	sel      *upstream.Selector
	ngx      *nginxctl.Manager
	web      fs.FS

	mu       sync.Mutex
	sessions map[string]session
}

type session struct {
	User    string
	Expires time.Time
}

func New(cfg config.Config, st *store.Store, reg *registry.Registry, limiters *queue.Manager, sel *upstream.Selector, ngx *nginxctl.Manager, web fs.FS) *Server {
	s := &Server{
		cfg: cfg, store: st, reg: reg, limiters: limiters, sel: sel, ngx: ngx, web: web,
		sessions: map[string]session{},
	}
	go s.gc()
	return s
}

func (s *Server) gc() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		s.mu.Lock()
		for k, v := range s.sessions {
			if now.After(v.Expires) {
				delete(s.sessions, k)
			}
		}
		s.mu.Unlock()
	}
}

func (s *Server) Handler(prefix string) http.Handler {
	inner := http.NewServeMux()
	inner.HandleFunc("/api/login", s.handleLogin)
	inner.HandleFunc("/api/logout", s.handleLogout)
	inner.HandleFunc("/api/me", s.auth(s.handleMe))
	inner.HandleFunc("/api/gateways", s.auth(s.handleGateways))
	inner.HandleFunc("/api/queues", s.auth(s.handleQueues))
	inner.HandleFunc("/api/graph", s.auth(s.handleGraph))
	inner.HandleFunc("/api/layout", s.auth(s.handleLayout))
	inner.HandleFunc("/api/nginx", s.auth(s.handleNginx))
	inner.HandleFunc("/api/nginx/preview", s.auth(s.handleNginxPreview))
	inner.HandleFunc("/api/nginx/apply", s.auth(s.handleNginxApply))
	inner.HandleFunc("/api/health", s.auth(s.handleHealth))
	inner.Handle("/", s.serveWeb(""))
	cut := strings.TrimSuffix(prefix, "/")
	if cut == "" {
		return inner
	}
	return http.StripPrefix(cut, inner)
}

func (s *Server) serveWeb(_ string) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(s.web))
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "" || r.URL.Path == "/index.html" {
			body, err := fs.ReadFile(s.web, "index.html")
			if err != nil {
				http.Error(w, "ui missing", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(body)
			return
		}
		fileServer.ServeHTTP(w, r)
	}
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("gw_admin")
		if err != nil || !s.valid(c.Value) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) valid(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	return ok && time.Now().Before(sess.Expires)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !s.store.CheckAdmin(body.Username, body.Password) {
		time.Sleep(200 * time.Millisecond)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "ورود نامعتبر"})
		return
	}
	token := newToken()
	s.mu.Lock()
	s.sessions[token] = session{User: body.Username, Expires: time.Now().Add(12 * time.Hour)}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: "gw_admin", Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 12 * 3600,
	})
	writeJSON(w, http.StatusOK, map[string]string{"user": body.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("gw_admin"); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "gw_admin", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"user": s.cfg.AdminUser})
}

func (s *Server) handleGateways(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id := r.URL.Query().Get("id")
		if id != "" {
			g, err := s.store.GetGateway(id)
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			s.attachQueue(g)
			writeJSON(w, http.StatusOK, g)
			return
		}
		list, err := s.store.ListGateways()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for i := range list {
			s.attachQueue(&list[i])
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost, http.MethodPut:
		var g models.Gateway
		if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(g.Name) == "" || strings.TrimSpace(g.Host) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "نام و دامنه الزامی است"})
			return
		}
		if err := s.assignListenPort(&g); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.store.SaveGateway(g); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = s.reg.Reload()
		if ns := s.loadNginx(); ns.AutoReload {
			if err := s.applyNginx(ns); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": "1", "listen_port": g.ListenPort, "nginx_error": err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": "1", "listen_port": g.ListenPort})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		if err := s.store.DeleteGateway(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.reg.Reload()
		writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleQueues(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListGateways()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]models.QueueSnapshot, 0, len(list))
	for _, g := range list {
		inf, wait, rej := s.limiters.Snapshot(g)
		out = append(out, models.QueueSnapshot{
			GatewayID: g.ID, Host: g.Host, Name: g.Name,
			Inflight: inf, Waiting: wait, Rejected: rej,
			MaxConc: g.MaxConcurrency, QueueSize: g.QueueSize, RPS: g.RPS,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListGateways()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	layout := s.store.LoadLayout()
	gph := models.Graph{Nodes: []models.GraphNode{}, Edges: []models.GraphEdge{}}
	for i, g := range list {
		domID := "d-" + g.ID
		st := "unknown"
		down := 0
		up := 0
		for _, u := range g.Upstreams {
			hs := s.sel.Snapshot(u, g.ID)
			switch hs.Status {
			case "up":
				up++
			case "down":
				down++
			}
		}
		if !g.Enabled {
			st = "off"
		} else if down > 0 && up == 0 {
			st = "down"
		} else if down > 0 {
			st = "degraded"
		} else if up > 0 {
			st = "up"
		}
		gph.Nodes = append(gph.Nodes, withLayout(models.GraphNode{
			ID: domID, Type: "domain", Label: g.Host, Subtitle: domainSubtitle(g), Status: st, GatewayID: g.ID,
			X: 80, Y: float64(80 + i*170),
		}, layout))
		for j, u := range g.Upstreams {
			sid := "s-" + u.ID
			hs := s.sel.Snapshot(u, g.ID)
			kind := "دامنه مقصد"
			if u.Kind == "local" {
				kind = "پورت محلی"
			}
			gph.Nodes = append(gph.Nodes, withLayout(models.GraphNode{
				ID: sid, Type: "service", Label: u.EffectiveURL(), Subtitle: kind, Status: hs.Status, GatewayID: g.ID,
				X: 520, Y: float64(80 + i*170 + j*90),
			}, layout))
			gph.Edges = append(gph.Edges, models.GraphEdge{
				ID: "e-" + g.ID + "-" + u.ID, From: domID, To: sid, Label: "proxy", Status: hs.Status,
			})
		}
	}
	writeJSON(w, http.StatusOK, gph)
}

func (s *Server) handleLayout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var pts []models.LayoutPoint
	if err := json.NewDecoder(r.Body).Decode(&pts); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.store.SaveLayout(pts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (s *Server) handleNginx(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.loadNginx())
	case http.MethodPost, http.MethodPut:
		var ns models.NginxSettings
		if err := json.NewDecoder(r.Body).Decode(&ns); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		raw, _ := json.Marshal(ns)
		if err := s.store.SetSetting("nginx", string(raw)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, ns)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNginxPreview(w http.ResponseWriter, r *http.Request) {
	ns := s.loadNginx()
	list, err := s.store.ListGateways()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := nginxctl.AssignGatewayPorts(ns, list, nginxctl.TCPPortFree); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cfg, err := s.ngx.Render(ns, list, s.cfg.AdminHost)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"config": cfg})
}

func (s *Server) handleNginxApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ns := s.loadNginx()
	if err := s.applyNginx(ns); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListGateways()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("probe") == "1" {
		for _, g := range list {
			for _, u := range g.Upstreams {
				if u.Enabled {
					s.sel.Probe(u, g.HealthPath)
				}
			}
		}
	}
	out := []models.HealthSnapshot{}
	for _, g := range list {
		for _, u := range g.Upstreams {
			out = append(out, s.sel.Snapshot(u, g.ID))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) loadNginx() models.NginxSettings {
	ns := nginxctl.Defaults(s.cfg)
	raw := s.store.GetSetting("nginx", "")
	if raw == "" {
		return ns
	}
	_ = json.Unmarshal([]byte(raw), &ns)
	if ns.ConfPath == "" {
		ns.ConfPath = s.cfg.NginxConfPath
	}
	if ns.TestCmd == "" {
		ns.TestCmd = s.cfg.NginxTestCmd
	}
	if ns.ReloadCmd == "" {
		ns.ReloadCmd = s.cfg.NginxReloadCmd
	}
	if ns.GatewayUpstream == "" {
		ns.GatewayUpstream = s.cfg.NginxGatewayAddr
	}
	return ns
}

func (s *Server) applyNginx(ns models.NginxSettings) error {
	list, err := s.store.ListGateways()
	if err != nil {
		return err
	}
	changed, err := nginxctl.AssignGatewayPorts(ns, list, nginxctl.TCPPortFree)
	if err != nil {
		return err
	}
	for id, port := range changed {
		if err := s.store.SetListenPort(id, port); err != nil {
			return err
		}
	}
	cfg, err := s.ngx.Render(ns, list, s.cfg.AdminHost)
	if err != nil {
		return err
	}
	if err := s.ngx.Apply(ns, cfg); err != nil {
		return err
	}
	s.openFirewallPorts(list)
	return nil
}

func (s *Server) assignListenPort(g *models.Gateway) error {
	ns := s.loadNginx()
	start, max := nginxctl.DomainPortRange(ns)
	keep := 0
	if g.ID != "" {
		if existing, err := s.store.GetGateway(g.ID); err == nil && existing.ListenPort > 0 {
			keep = existing.ListenPort
			if g.ListenPort <= 0 {
				g.ListenPort = existing.ListenPort
			}
		}
	}
	port, err := nginxctl.PickPort(start, max, g.ListenPort, s.store.UsedListenPorts(g.ID), nginxctl.ReservedPorts(ns), keep, nginxctl.TCPPortFree)
	if err != nil {
		return err
	}
	g.ListenPort = port
	return nil
}

func (s *Server) openFirewallPorts(list []models.Gateway) {
	seen := map[int]struct{}{}
	for _, g := range list {
		if !g.Enabled || g.ListenPort <= 0 {
			continue
		}
		if _, ok := seen[g.ListenPort]; ok {
			continue
		}
		seen[g.ListenPort] = struct{}{}
		nginxctl.TryOpenHostPort(g.ListenPort)
	}
}

func (s *Server) attachQueue(g *models.Gateway) {
	if g.Stats == nil {
		g.Stats = &models.Stats{}
	}
	inf, wait, rej := s.limiters.Snapshot(*g)
	g.Stats.Inflight = inf
	g.Stats.Waiting = wait
	g.Stats.QueueRej = rej
}

func domainSubtitle(g models.Gateway) string {
	if g.ListenPort > 0 {
		if g.Name != "" {
			return fmt.Sprintf("%s  :%d", g.Name, g.ListenPort)
		}
		return fmt.Sprintf(":%d", g.ListenPort)
	}
	return g.Name
}

func withLayout(n models.GraphNode, layout map[string]models.LayoutPoint) models.GraphNode {
	if p, ok := layout[n.ID]; ok {
		n.X, n.Y = p.X, p.Y
	}
	return n
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
