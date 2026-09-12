package models

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Gateway struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Host             string        `json:"host"`
	Enabled          bool          `json:"enabled"`
	LBStrategy       string        `json:"lb_strategy"`
	MaxConcurrency   int           `json:"max_concurrency"`
	QueueSize        int           `json:"queue_size"`
	QueueTimeoutMS   int           `json:"queue_timeout_ms"`
	RPS              int           `json:"rps"`
	Sensitive        bool          `json:"sensitive"`
	CORSAllowOrigin  string        `json:"cors_allow_origin"`
	Websocket        bool          `json:"websocket"`
	ClientMaxBody    string        `json:"client_max_body"`
	ProxyReadTimeout int           `json:"proxy_read_timeout"`
	ProxySendTimeout int           `json:"proxy_send_timeout"`
	NginxExtra       string        `json:"nginx_extra"`
	HealthPath       string        `json:"health_path"`
	ListenPort       int           `json:"listen_port"`
	AllowedOrigins   []string      `json:"allowed_origins"`
	AccessTokens     []AccessToken `json:"access_tokens,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	Upstreams        []Upstream    `json:"upstreams,omitempty"`
	Routes           []Route       `json:"routes,omitempty"`
	Stats            *Stats        `json:"stats,omitempty"`
}

type AccessToken struct {
	ID        string    `json:"id"`
	GatewayID string    `json:"gateway_id,omitempty"`
	Name      string    `json:"name"`
	Token     string    `json:"token"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type Upstream struct {
	ID         string `json:"id"`
	GatewayID  string `json:"gateway_id"`
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
	Scheme     string `json:"scheme"`
	HealthPath string `json:"health_path"`
	Weight     int    `json:"weight"`
	Enabled    bool   `json:"enabled"`
}

func (u *Upstream) Normalize() {
	u.Kind = strings.ToLower(strings.TrimSpace(u.Kind))
	if u.Kind == "" {
		if u.TargetPort > 0 {
			u.Kind = "local"
		} else {
			u.Kind = "remote"
		}
	}
	u.Scheme = strings.ToLower(strings.TrimSpace(u.Scheme))
	u.TargetHost = strings.TrimSpace(u.TargetHost)
	if u.Kind == "local" {
		if u.TargetHost == "" {
			u.TargetHost = "127.0.0.1"
		}
		if u.Scheme == "" {
			u.Scheme = "http"
		}
		if u.TargetPort <= 0 {
			u.TargetPort = 80
		}
		u.URL = fmt.Sprintf("%s://%s:%d", u.Scheme, u.TargetHost, u.TargetPort)
		return
	}
	u.URL = strings.TrimRight(strings.TrimSpace(u.URL), "/")
	if u.URL == "" {
		return
	}
	parsed, err := url.Parse(u.URL)
	if err != nil {
		return
	}
	if u.Scheme == "" {
		u.Scheme = parsed.Scheme
	}
	if u.TargetHost == "" {
		u.TargetHost = parsed.Hostname()
	}
	if u.TargetPort <= 0 {
		if p := parsed.Port(); p != "" {
			u.TargetPort, _ = strconv.Atoi(p)
		} else if parsed.Scheme == "https" {
			u.TargetPort = 443
		} else {
			u.TargetPort = 80
		}
	}
}

func (u Upstream) EffectiveURL() string {
	u.Normalize()
	return u.URL
}

type Route struct {
	ID          string            `json:"id"`
	GatewayID   string            `json:"gateway_id"`
	PathPrefix  string            `json:"path_prefix"`
	PathRegex   string            `json:"path_regex"`
	StripPrefix string            `json:"strip_prefix"`
	AddPrefix   string            `json:"add_prefix"`
	SetQuery    map[string]string `json:"set_query"`
	RemoveQuery []string          `json:"remove_query"`
	SetHeaders  map[string]string `json:"set_headers"`
	Priority    int               `json:"priority"`
}

type Stats struct {
	Total    int64 `json:"total"`
	Last24h  int64 `json:"last_24h"`
	Rejected int64 `json:"rejected"`
	Errors   int64 `json:"errors"`
	Inflight int64 `json:"inflight"`
	Waiting  int64 `json:"waiting"`
	QueueRej int64 `json:"queue_rejected"`
}

type QueueSnapshot struct {
	GatewayID string `json:"gateway_id"`
	Host      string `json:"host"`
	Name      string `json:"name"`
	Inflight  int64  `json:"inflight"`
	Waiting   int64  `json:"waiting"`
	Rejected  int64  `json:"rejected"`
	MaxConc   int    `json:"max_concurrency"`
	QueueSize int    `json:"queue_size"`
	RPS       int    `json:"rps"`
}

type NginxSettings struct {
	Managed          bool   `json:"managed"`
	AutoReload       bool   `json:"auto_reload"`
	ConfPath         string `json:"conf_path"`
	TestCmd          string `json:"test_cmd"`
	ReloadCmd        string `json:"reload_cmd"`
	// Shared443: همه دامنه‌ها روی یک listen 443 (مدل کارفرما / میکروسرویس‌ها).
	// false = هر دامنه پورت عمومی جدا در محدوده DomainPortStart..Max.
	Shared443        bool   `json:"shared_443"`
	ListenHTTP       int    `json:"listen_http"`
	ListenHTTPS      int    `json:"listen_https"`
	ListenAdminHTTPS int    `json:"listen_admin_https"`
	DomainPortStart  int    `json:"domain_port_start"`
	DomainPortMax    int    `json:"domain_port_max"`
	SSLCert          string `json:"ssl_cert"`
	SSLKey           string `json:"ssl_key"`
	RedirectHTTP     bool   `json:"redirect_http"`
	ClientMaxBody    string `json:"client_max_body"`
	ProxyReadTimeout int    `json:"proxy_read_timeout"`
	ProxySendTimeout int    `json:"proxy_send_timeout"`
	Websocket        bool   `json:"websocket"`
	GatewayUpstream  string `json:"gateway_upstream"`
}

type HealthSnapshot struct {
	UpstreamID string    `json:"upstream_id"`
	GatewayID  string    `json:"gateway_id"`
	Target     string    `json:"target"`
	Status     string    `json:"status"`
	Healthy    bool      `json:"healthy"`
	LatencyMS  int64     `json:"latency_ms"`
	StatusCode int       `json:"status_code"`
	LastError  string    `json:"last_error"`
	LastCheck  time.Time `json:"last_check"`
	Fails      int64     `json:"fails"`
}

type GraphNode struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Label     string  `json:"label"`
	Subtitle  string  `json:"subtitle"`
	Status    string  `json:"status"`
	GatewayID string  `json:"gateway_id,omitempty"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
}

type GraphEdge struct {
	ID     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Label  string `json:"label"`
	Status string `json:"status"`
}

type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type LayoutPoint struct {
	ID string  `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}
