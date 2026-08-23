package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"sabzevar.ir/gateway-core/internal/config"
	"sabzevar.ir/gateway-core/internal/models"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.migrateExtra(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS gateways (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  host TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1,
  lb_strategy TEXT NOT NULL DEFAULT 'single',
  max_concurrency INTEGER NOT NULL DEFAULT 100,
  queue_size INTEGER NOT NULL DEFAULT 200,
  queue_timeout_ms INTEGER NOT NULL DEFAULT 5000,
  rps INTEGER NOT NULL DEFAULT 0,
  sensitive INTEGER NOT NULL DEFAULT 0,
  cors_allow_origin TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS upstreams (
  id TEXT PRIMARY KEY,
  gateway_id TEXT NOT NULL,
  url TEXT NOT NULL,
  weight INTEGER NOT NULL DEFAULT 1,
  enabled INTEGER NOT NULL DEFAULT 1,
  FOREIGN KEY(gateway_id) REFERENCES gateways(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS routes (
  id TEXT PRIMARY KEY,
  gateway_id TEXT NOT NULL,
  path_prefix TEXT NOT NULL DEFAULT '/',
  path_regex TEXT NOT NULL DEFAULT '',
  strip_prefix TEXT NOT NULL DEFAULT '',
  add_prefix TEXT NOT NULL DEFAULT '',
  set_query TEXT NOT NULL DEFAULT '{}',
  remove_query TEXT NOT NULL DEFAULT '[]',
  set_headers TEXT NOT NULL DEFAULT '{}',
  priority INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY(gateway_id) REFERENCES gateways(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS request_stats (
  gateway_id TEXT NOT NULL,
  bucket TEXT NOT NULL,
  count INTEGER NOT NULL DEFAULT 0,
  rejected INTEGER NOT NULL DEFAULT 0,
  errors INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (gateway_id, bucket)
);
CREATE TABLE IF NOT EXISTS admin_users (
  username TEXT PRIMARY KEY,
  password_hash TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS ui_layout (
  id TEXT PRIMARY KEY,
  x REAL NOT NULL,
  y REAL NOT NULL
);
`)
	return err
}

func (s *Store) migrateExtra() error {
	cols := []string{
		"ALTER TABLE gateways ADD COLUMN websocket INTEGER NOT NULL DEFAULT 1",
		"ALTER TABLE gateways ADD COLUMN client_max_body TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE gateways ADD COLUMN proxy_read_timeout INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE gateways ADD COLUMN proxy_send_timeout INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE gateways ADD COLUMN nginx_extra TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE gateways ADD COLUMN health_path TEXT NOT NULL DEFAULT '/'",
		"ALTER TABLE upstreams ADD COLUMN kind TEXT NOT NULL DEFAULT 'remote'",
		"ALTER TABLE upstreams ADD COLUMN target_host TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE upstreams ADD COLUMN target_port INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE upstreams ADD COLUMN scheme TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE upstreams ADD COLUMN health_path TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE gateways ADD COLUMN allowed_origins TEXT NOT NULL DEFAULT '[]'",
	}
	for _, q := range cols {
		_, _ = s.db.Exec(q)
	}
	_, _ = s.db.Exec(`
CREATE TABLE IF NOT EXISTS access_tokens (
  id TEXT PRIMARY KEY,
  gateway_id TEXT NOT NULL,
  name TEXT NOT NULL,
  token TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  FOREIGN KEY(gateway_id) REFERENCES gateways(id) ON DELETE CASCADE
);`)
	return nil
}

func (s *Store) Seed(cfg config.Config) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
INSERT INTO admin_users(username, password_hash) VALUES(?, ?)
ON CONFLICT(username) DO UPDATE SET password_hash=excluded.password_hash
`, cfg.AdminUser, string(hash))
	if err != nil {
		return err
	}

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM gateways`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	now := time.Now().UTC()
	mapID := uuid.NewString()
	loginID := uuid.NewString()

	if err := s.insertGateway(models.Gateway{
		ID: mapID, Name: "نقشه", Host: "map-gateway.sabzevar.ir", Enabled: true,
		LBStrategy: "single", MaxConcurrency: 200, QueueSize: 400, QueueTimeoutMS: 3000, RPS: 0,
		Sensitive: false, CORSAllowOrigin: "*", Websocket: true, HealthPath: "/", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return err
	}
	if err := s.insertUpstream(models.Upstream{ID: uuid.NewString(), GatewayID: mapID, URL: "https://geo.sabzevar.ir", Weight: 1, Enabled: true}); err != nil {
		return err
	}
	if err := s.insertRoute(models.Route{
		ID: uuid.NewString(), GatewayID: mapID, PathPrefix: "/", Priority: 0,
		SetQuery: map[string]string{}, RemoveQuery: []string{},
		SetHeaders: map[string]string{"X-Forwarded-Gateway": "map"},
	}); err != nil {
		return err
	}

	if err := s.insertGateway(models.Gateway{
		ID: loginID, Name: "لاگین", Host: "apisrv-gatewaylogin.sabzevar.ir", Enabled: true,
		LBStrategy: "single", MaxConcurrency: 80, QueueSize: 160, QueueTimeoutMS: 5000, RPS: 20,
		Sensitive: true, CORSAllowOrigin: "", Websocket: false, HealthPath: "/", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return err
	}
	if err := s.insertUpstream(models.Upstream{ID: uuid.NewString(), GatewayID: loginID, URL: "https://apisrv.sabzevar.ir", Weight: 1, Enabled: true}); err != nil {
		return err
	}
	if err := s.insertRoute(models.Route{
		ID: uuid.NewString(), GatewayID: loginID, PathPrefix: "/", Priority: 0,
		SetQuery: map[string]string{}, RemoveQuery: []string{},
		SetHeaders: map[string]string{"X-Forwarded-Gateway": "login"},
	}); err != nil {
		return err
	}
	return nil
}

func (s *Store) CheckAdmin(username, password string) bool {
	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM admin_users WHERE username = ?`, username).Scan(&hash)
	if err != nil {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *Store) ListGateways() ([]models.Gateway, error) {
	rows, err := s.db.Query(`SELECT id, name, host, enabled, lb_strategy, max_concurrency, queue_size, queue_timeout_ms, rps, sensitive, cors_allow_origin, websocket, client_max_body, proxy_read_timeout, proxy_send_timeout, nginx_extra, health_path, allowed_origins, created_at, updated_at FROM gateways ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Gateway{}
	for rows.Next() {
		g, err := scanGateway(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	for i := range out {
		if err := s.loadChildren(&out[i]); err != nil {
			return nil, err
		}
		st, err := s.GatewayStats(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Stats = &st
	}
	return out, rows.Err()
}

func (s *Store) GetGateway(id string) (*models.Gateway, error) {
	row := s.db.QueryRow(`SELECT id, name, host, enabled, lb_strategy, max_concurrency, queue_size, queue_timeout_ms, rps, sensitive, cors_allow_origin, websocket, client_max_body, proxy_read_timeout, proxy_send_timeout, nginx_extra, health_path, allowed_origins, created_at, updated_at FROM gateways WHERE id = ?`, id)
	g, err := scanGateway(row)
	if err != nil {
		return nil, err
	}
	if err := s.loadChildren(&g); err != nil {
		return nil, err
	}
	st, err := s.GatewayStats(g.ID)
	if err != nil {
		return nil, err
	}
	g.Stats = &st
	return &g, nil
}

func (s *Store) SaveGateway(g models.Gateway) error {
	now := time.Now().UTC()
	if g.ID == "" {
		g.ID = uuid.NewString()
		g.CreatedAt = now
	}
	g.UpdatedAt = now
	g.Host = strings.ToLower(strings.TrimSpace(g.Host))
	if g.LBStrategy == "" {
		g.LBStrategy = "single"
	}
	if g.MaxConcurrency <= 0 {
		g.MaxConcurrency = 100
	}
	if g.QueueSize < 0 {
		g.QueueSize = 0
	}
	if g.QueueTimeoutMS <= 0 {
		g.QueueTimeoutMS = 5000
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	originsJSON, _ := json.Marshal(cleanOrigins(g.AllowedOrigins))
	_, err = tx.Exec(`
INSERT INTO gateways(id, name, host, enabled, lb_strategy, max_concurrency, queue_size, queue_timeout_ms, rps, sensitive, cors_allow_origin, websocket, client_max_body, proxy_read_timeout, proxy_send_timeout, nginx_extra, health_path, allowed_origins, created_at, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name, host=excluded.host, enabled=excluded.enabled, lb_strategy=excluded.lb_strategy,
  max_concurrency=excluded.max_concurrency, queue_size=excluded.queue_size, queue_timeout_ms=excluded.queue_timeout_ms,
  rps=excluded.rps, sensitive=excluded.sensitive, cors_allow_origin=excluded.cors_allow_origin,
  websocket=excluded.websocket, client_max_body=excluded.client_max_body, proxy_read_timeout=excluded.proxy_read_timeout,
  proxy_send_timeout=excluded.proxy_send_timeout, nginx_extra=excluded.nginx_extra, health_path=excluded.health_path,
  allowed_origins=excluded.allowed_origins, updated_at=excluded.updated_at
`, g.ID, g.Name, g.Host, boolInt(g.Enabled), g.LBStrategy, g.MaxConcurrency, g.QueueSize, g.QueueTimeoutMS, g.RPS, boolInt(g.Sensitive), g.CORSAllowOrigin, boolInt(g.Websocket), g.ClientMaxBody, g.ProxyReadTimeout, g.ProxySendTimeout, g.NginxExtra, g.HealthPath, string(originsJSON), rfc(g.CreatedAt), rfc(g.UpdatedAt))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM upstreams WHERE gateway_id = ?`, g.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM routes WHERE gateway_id = ?`, g.ID); err != nil {
		return err
	}
	for _, u := range g.Upstreams {
		if u.ID == "" {
			u.ID = uuid.NewString()
		}
		if u.Weight <= 0 {
			u.Weight = 1
		}
		u.Normalize()
		if _, err := tx.Exec(`INSERT INTO upstreams(id, gateway_id, url, weight, enabled, kind, target_host, target_port, scheme, health_path) VALUES(?,?,?,?,?,?,?,?,?,?)`, u.ID, g.ID, strings.TrimRight(u.URL, "/"), u.Weight, boolInt(u.Enabled), u.Kind, u.TargetHost, u.TargetPort, u.Scheme, u.HealthPath); err != nil {
			return err
		}
	}
	for _, r := range g.Routes {
		if r.ID == "" {
			r.ID = uuid.NewString()
		}
		if r.PathPrefix == "" {
			r.PathPrefix = "/"
		}
		sq, _ := json.Marshal(r.SetQuery)
		rq, _ := json.Marshal(r.RemoveQuery)
		sh, _ := json.Marshal(r.SetHeaders)
		if _, err := tx.Exec(`INSERT INTO routes(id, gateway_id, path_prefix, path_regex, strip_prefix, add_prefix, set_query, remove_query, set_headers, priority) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			r.ID, g.ID, r.PathPrefix, r.PathRegex, r.StripPrefix, r.AddPrefix, string(sq), string(rq), string(sh), r.Priority); err != nil {
			return err
		}
	}
	oldTok := map[string]string{}
	trows, err := tx.Query(`SELECT id, token FROM access_tokens WHERE gateway_id = ?`, g.ID)
	if err != nil {
		return err
	}
	for trows.Next() {
		var id, tok string
		if err := trows.Scan(&id, &tok); err != nil {
			trows.Close()
			return err
		}
		oldTok[id] = tok
	}
	trows.Close()
	if _, err := tx.Exec(`DELETE FROM access_tokens WHERE gateway_id = ?`, g.ID); err != nil {
		return err
	}
	for _, t := range g.AccessTokens {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		if t.ID == "" {
			t.ID = uuid.NewString()
		}
		token := strings.TrimSpace(t.Token)
		if token == "" {
			token = oldTok[t.ID]
		}
		if token == "" {
			token = randomToken()
		}
		if t.CreatedAt.IsZero() {
			t.CreatedAt = now
		}
		if _, err := tx.Exec(`INSERT INTO access_tokens(id, gateway_id, name, token, enabled, created_at) VALUES(?,?,?,?,?,?)`,
			t.ID, g.ID, name, token, boolInt(t.Enabled), rfc(t.CreatedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteGateway(id string) error {
	_, err := s.db.Exec(`DELETE FROM gateways WHERE id = ?`, id)
	return err
}

func (s *Store) IncrCount(gatewayID string) {
	s.bump(gatewayID, "count")
}

func (s *Store) IncrRejected(gatewayID string) {
	s.bump(gatewayID, "rejected")
}

func (s *Store) IncrErrors(gatewayID string) {
	s.bump(gatewayID, "errors")
}

func (s *Store) bump(gatewayID, col string) {
	bucket := time.Now().UTC().Format("2006-01-02-15")
	_, _ = s.db.Exec(`INSERT INTO request_stats(gateway_id, bucket, count, rejected, errors) VALUES(?, ?, 0, 0, 0)
ON CONFLICT(gateway_id, bucket) DO NOTHING`, gatewayID, bucket)
	_, _ = s.db.Exec(`UPDATE request_stats SET `+col+` = `+col+` + 1 WHERE gateway_id = ? AND bucket = ?`, gatewayID, bucket)
}

func (s *Store) GatewayStats(id string) (models.Stats, error) {
	var st models.Stats
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(count),0), COALESCE(SUM(rejected),0), COALESCE(SUM(errors),0) FROM request_stats WHERE gateway_id = ?`, id).Scan(&st.Total, &st.Rejected, &st.Errors)
	since := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02-15")
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(count),0) FROM request_stats WHERE gateway_id = ? AND bucket >= ?`, id, since).Scan(&st.Last24h)
	return st, nil
}

func (s *Store) insertGateway(g models.Gateway) error {
	originsJSON, _ := json.Marshal(cleanOrigins(g.AllowedOrigins))
	_, err := s.db.Exec(`INSERT INTO gateways(id, name, host, enabled, lb_strategy, max_concurrency, queue_size, queue_timeout_ms, rps, sensitive, cors_allow_origin, websocket, client_max_body, proxy_read_timeout, proxy_send_timeout, nginx_extra, health_path, allowed_origins, created_at, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, g.ID, g.Name, g.Host, boolInt(g.Enabled), g.LBStrategy, g.MaxConcurrency, g.QueueSize, g.QueueTimeoutMS, g.RPS, boolInt(g.Sensitive), g.CORSAllowOrigin, boolInt(g.Websocket), g.ClientMaxBody, g.ProxyReadTimeout, g.ProxySendTimeout, g.NginxExtra, g.HealthPath, string(originsJSON), rfc(g.CreatedAt), rfc(g.UpdatedAt))
	return err
}

func (s *Store) insertUpstream(u models.Upstream) error {
	u.Normalize()
	_, err := s.db.Exec(`INSERT INTO upstreams(id, gateway_id, url, weight, enabled, kind, target_host, target_port, scheme, health_path) VALUES(?,?,?,?,?,?,?,?,?,?)`, u.ID, u.GatewayID, strings.TrimRight(u.URL, "/"), u.Weight, boolInt(u.Enabled), u.Kind, u.TargetHost, u.TargetPort, u.Scheme, u.HealthPath)
	return err
}

func (s *Store) insertRoute(r models.Route) error {
	sq, _ := json.Marshal(r.SetQuery)
	rq, _ := json.Marshal(r.RemoveQuery)
	sh, _ := json.Marshal(r.SetHeaders)
	_, err := s.db.Exec(`INSERT INTO routes(id, gateway_id, path_prefix, path_regex, strip_prefix, add_prefix, set_query, remove_query, set_headers, priority) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.GatewayID, r.PathPrefix, r.PathRegex, r.StripPrefix, r.AddPrefix, string(sq), string(rq), string(sh), r.Priority)
	return err
}

func (s *Store) loadChildren(g *models.Gateway) error {
	rows, err := s.db.Query(`SELECT id, gateway_id, url, weight, enabled, kind, target_host, target_port, scheme, health_path FROM upstreams WHERE gateway_id = ?`, g.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var u models.Upstream
		var en int
		if err := rows.Scan(&u.ID, &u.GatewayID, &u.URL, &u.Weight, &en, &u.Kind, &u.TargetHost, &u.TargetPort, &u.Scheme, &u.HealthPath); err != nil {
			return err
		}
		u.Enabled = en == 1
		u.Normalize()
		g.Upstreams = append(g.Upstreams, u)
	}
	rrows, err := s.db.Query(`SELECT id, gateway_id, path_prefix, path_regex, strip_prefix, add_prefix, set_query, remove_query, set_headers, priority FROM routes WHERE gateway_id = ? ORDER BY priority DESC`, g.ID)
	if err != nil {
		return err
	}
	defer rrows.Close()
	for rrows.Next() {
		var r models.Route
		var sq, rq, sh string
		if err := rrows.Scan(&r.ID, &r.GatewayID, &r.PathPrefix, &r.PathRegex, &r.StripPrefix, &r.AddPrefix, &sq, &rq, &sh, &r.Priority); err != nil {
			return err
		}
		_ = json.Unmarshal([]byte(sq), &r.SetQuery)
		_ = json.Unmarshal([]byte(rq), &r.RemoveQuery)
		_ = json.Unmarshal([]byte(sh), &r.SetHeaders)
		if r.SetQuery == nil {
			r.SetQuery = map[string]string{}
		}
		if r.SetHeaders == nil {
			r.SetHeaders = map[string]string{}
		}
		g.Routes = append(g.Routes, r)
	}
	trows, err := s.db.Query(`SELECT id, gateway_id, name, token, enabled, created_at FROM access_tokens WHERE gateway_id = ? ORDER BY created_at`, g.ID)
	if err != nil {
		return err
	}
	defer trows.Close()
	for trows.Next() {
		var t models.AccessToken
		var en int
		var created string
		if err := trows.Scan(&t.ID, &t.GatewayID, &t.Name, &t.Token, &en, &created); err != nil {
			return err
		}
		t.Enabled = en == 1
		t.CreatedAt, _ = time.Parse(time.RFC3339, created)
		g.AccessTokens = append(g.AccessTokens, t)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanGateway(sc scanner) (models.Gateway, error) {
	var g models.Gateway
	var en, sens, ws int
	var created, updated string
	var originsJSON string
	err := sc.Scan(&g.ID, &g.Name, &g.Host, &en, &g.LBStrategy, &g.MaxConcurrency, &g.QueueSize, &g.QueueTimeoutMS, &g.RPS, &sens, &g.CORSAllowOrigin, &ws, &g.ClientMaxBody, &g.ProxyReadTimeout, &g.ProxySendTimeout, &g.NginxExtra, &g.HealthPath, &originsJSON, &created, &updated)
	if err != nil {
		return g, err
	}
	g.Enabled = en == 1
	g.Sensitive = sens == 1
	g.Websocket = ws == 1
	if originsJSON != "" {
		_ = json.Unmarshal([]byte(originsJSON), &g.AllowedOrigins)
	}
	if g.AllowedOrigins == nil {
		g.AllowedOrigins = []string{}
	}
	g.CreatedAt, _ = time.Parse(time.RFC3339, created)
	g.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return g, nil
}

func cleanOrigins(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return out
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return uuid.NewString()
	}
	return hex.EncodeToString(b)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func rfc(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *Store) GetSetting(key, fallback string) string {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err != nil || v == "" {
		return fallback
	}
	return v
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) LoadLayout() map[string]models.LayoutPoint {
	out := map[string]models.LayoutPoint{}
	rows, err := s.db.Query(`SELECT id, x, y FROM ui_layout`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p models.LayoutPoint
		if rows.Scan(&p.ID, &p.X, &p.Y) == nil {
			out[p.ID] = p
		}
	}
	return out
}

func (s *Store) SaveLayout(points []models.LayoutPoint) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range points {
		if p.ID == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO ui_layout(id, x, y) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET x=excluded.x, y=excluded.y`, p.ID, p.X, p.Y); err != nil {
			return err
		}
	}
	return tx.Commit()
}
