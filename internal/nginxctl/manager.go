package nginxctl

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"sabzevar.ir/gateway-core/internal/config"
	"sabzevar.ir/gateway-core/internal/models"
)

var (
	hostRe  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]+$`)
	bodyRe  = regexp.MustCompile(`^[0-9]+[kKmMgG]?$`)
	extraRe = regexp.MustCompile(`(?i)(include|perl_|js_|lua_|load_module)`)
)

type Manager struct {
	cfg config.Config
}

func New(cfg config.Config) *Manager {
	return &Manager{cfg: cfg}
}

func Defaults(cfg config.Config) models.NginxSettings {
	return models.NginxSettings{
		Managed:          true,
		AutoReload:       false,
		ConfPath:         cfg.NginxConfPath,
		TestCmd:          cfg.NginxTestCmd,
		ReloadCmd:        cfg.NginxReloadCmd,
		ListenHTTP:       80,
		ListenHTTPS:      443,
		ListenAdminHTTPS: 8003,
		SSLCert:          "/etc/pki/nginx/fullchain.pem",
		SSLKey:           "/etc/pki/nginx/privkey.pem",
		RedirectHTTP:     true,
		ClientMaxBody:    "20m",
		ProxyReadTimeout: 120,
		ProxySendTimeout: 120,
		Websocket:        true,
		GatewayUpstream:  cfg.NginxGatewayAddr,
	}
}

func (m *Manager) Render(settings models.NginxSettings, gateways []models.Gateway, adminHost string) (string, error) {
	if settings.ListenHTTP <= 0 {
		settings.ListenHTTP = 80
	}
	if settings.ListenHTTPS <= 0 {
		settings.ListenHTTPS = 443
	}
	if settings.ListenAdminHTTPS <= 0 {
		settings.ListenAdminHTTPS = 8003
	}
	if settings.GatewayUpstream == "" {
		settings.GatewayUpstream = "127.0.0.1:8002"
	}
	if settings.ClientMaxBody == "" || !bodyRe.MatchString(settings.ClientMaxBody) {
		settings.ClientMaxBody = "20m"
	}
	if !validAddr(settings.GatewayUpstream) {
		return "", fmt.Errorf("آدرس upstream گیت‌وی نامعتبر است")
	}
	if settings.SSLCert != "" && !safePath(settings.SSLCert) {
		return "", fmt.Errorf("مسیر گواهی نامعتبر است")
	}
	if settings.SSLKey != "" && !safePath(settings.SSLKey) {
		return "", fmt.Errorf("مسیر کلید نامعتبر است")
	}

	var b strings.Builder
	b.WriteString("# managed by gateway-core — do not edit by hand\n")
	b.WriteString("map $http_upgrade $connection_upgrade {\n    default upgrade;\n    ''      close;\n}\n\n")
	b.WriteString("upstream gateway_core {\n    server " + settings.GatewayUpstream + ";\n    keepalive 32;\n}\n\n")
	b.WriteString(fmt.Sprintf(`server {
    listen %d default_server;
    listen [::]:%d default_server;
    server_name _;
    client_max_body_size 2m;
    location / {
        proxy_pass http://gateway_core;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

`, settings.ListenHTTP, settings.ListenHTTP))

	hosts := make([]string, 0, len(gateways)+1)
	seen := map[string]bool{}
	addHost := func(h string) {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" || seen[h] {
			return
		}
		if !hostRe.MatchString(h) {
			return
		}
		seen[h] = true
		hosts = append(hosts, h)
	}
	for _, g := range gateways {
		if g.Enabled {
			addHost(g.Host)
		}
	}
	if len(hosts) == 0 && (adminHost == "" || !hostRe.MatchString(adminHost)) {
		return "", fmt.Errorf("هیچ دامنه‌ای برای Nginx تعریف نشده")
	}

	if settings.RedirectHTTP && len(hosts) > 0 {
		b.WriteString("server {\n")
		b.WriteString(fmt.Sprintf("    listen %d;\n    listen [::]:%d;\n", settings.ListenHTTP, settings.ListenHTTP))
		b.WriteString("    server_name " + strings.Join(hosts, " ") + ";\n")
		b.WriteString("    return 301 https://$host$request_uri;\n}\n\n")
	}
	if settings.RedirectHTTP && adminHost != "" && hostRe.MatchString(adminHost) {
		b.WriteString("server {\n")
		b.WriteString(fmt.Sprintf("    listen %d;\n    listen [::]:%d;\n", settings.ListenHTTP, settings.ListenHTTP))
		b.WriteString("    server_name " + adminHost + ";\n")
		b.WriteString(fmt.Sprintf("    return 301 https://$host:%d$request_uri;\n}\n\n", settings.ListenAdminHTTPS))
	}

	for _, g := range gateways {
		if !g.Enabled || !hostRe.MatchString(g.Host) {
			continue
		}
		body := settings.ClientMaxBody
		if g.ClientMaxBody != "" && bodyRe.MatchString(g.ClientMaxBody) {
			body = g.ClientMaxBody
		}
		readTO := settings.ProxyReadTimeout
		if g.ProxyReadTimeout > 0 {
			readTO = g.ProxyReadTimeout
		}
		sendTO := settings.ProxySendTimeout
		if g.ProxySendTimeout > 0 {
			sendTO = g.ProxySendTimeout
		}
		ws := settings.Websocket || g.Websocket
		b.WriteString("# " + g.Name + " → ")
		if len(g.Upstreams) > 0 {
			b.WriteString(g.Upstreams[0].EffectiveURL())
		}
		b.WriteString("\nserver {\n")
		b.WriteString(fmt.Sprintf("    listen %d ssl;\n    listen [::]:%d ssl;\n    http2 on;\n", settings.ListenHTTPS, settings.ListenHTTPS))
		b.WriteString("    server_name " + g.Host + ";\n")
		if settings.SSLCert != "" {
			b.WriteString("    ssl_certificate     " + settings.SSLCert + ";\n")
			b.WriteString("    ssl_certificate_key " + settings.SSLKey + ";\n")
		}
		b.WriteString("    client_max_body_size " + body + ";\n")
		b.WriteString("    location / {\n")
		b.WriteString("        proxy_pass http://gateway_core;\n")
		b.WriteString("        proxy_http_version 1.1;\n")
		b.WriteString("        proxy_set_header Host $host;\n")
		b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
		b.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
		if ws {
			b.WriteString("        proxy_set_header Upgrade $http_upgrade;\n")
			b.WriteString("        proxy_set_header Connection $connection_upgrade;\n")
		}
		b.WriteString(fmt.Sprintf("        proxy_read_timeout %ds;\n        proxy_send_timeout %ds;\n", readTO, sendTO))
		b.WriteString("        proxy_buffering off;\n")
		if extra := sanitizeExtra(g.NginxExtra); extra != "" {
			for _, line := range strings.Split(extra, "\n") {
				b.WriteString("        " + line + "\n")
			}
		}
		b.WriteString("    }\n}\n\n")
	}

	if adminHost != "" && hostRe.MatchString(adminHost) {
		b.WriteString("# پنل ادمین HTTPS\nserver {\n")
		b.WriteString(fmt.Sprintf("    listen %d ssl;\n    listen [::]:%d ssl;\n    http2 on;\n", settings.ListenAdminHTTPS, settings.ListenAdminHTTPS))
		b.WriteString("    server_name " + adminHost + ";\n")
		if settings.SSLCert != "" {
			b.WriteString("    ssl_certificate     " + settings.SSLCert + ";\n")
			b.WriteString("    ssl_certificate_key " + settings.SSLKey + ";\n")
		}
		b.WriteString("    client_max_body_size 2m;\n")
		b.WriteString("    location / {\n")
		b.WriteString("        proxy_pass http://gateway_core;\n")
		b.WriteString("        proxy_http_version 1.1;\n")
		b.WriteString("        proxy_set_header Host $host;\n")
		b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
		b.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
		b.WriteString("    }\n}\n")
	}
	return b.String(), nil
}

func (m *Manager) Apply(settings models.NginxSettings, content string) error {
	if strings.TrimSpace(settings.ConfPath) == "" {
		return fmt.Errorf("مسیر فایل Nginx خالی است")
	}
	if !safePath(settings.ConfPath) {
		return fmt.Errorf("مسیر فایل Nginx نامعتبر است")
	}
	if err := os.MkdirAll(filepath.Dir(settings.ConfPath), 0o755); err != nil {
		return err
	}
	tmp := settings.ConfPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, settings.ConfPath); err != nil {
		return err
	}
	if skipCmd(settings.TestCmd) && skipCmd(settings.ReloadCmd) {
		return nil
	}
	if !skipCmd(settings.TestCmd) {
		if out, err := run(settings.TestCmd); err != nil {
			return fmt.Errorf("nginx -t: %v\n%s", err, out)
		}
	}
	if !skipCmd(settings.ReloadCmd) {
		if out, err := run(settings.ReloadCmd); err != nil {
			return fmt.Errorf("nginx reload: %v\n%s", err, out)
		}
	}
	return nil
}

func skipCmd(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "" || s == "-" || s == "none"
}

func run(cmdline string) ([]byte, error) {
	parts := strings.Fields(strings.TrimSpace(cmdline))
	if len(parts) == 0 {
		return nil, fmt.Errorf("دستور خالی است")
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	return cmd.CombinedOutput()
}

func sanitizeExtra(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if extraRe.MatchString(line) || strings.Contains(line, "{") || strings.Contains(line, "}") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func safePath(p string) bool {
	if p == "" || strings.Contains(p, "..") || strings.ContainsAny(p, " \t\n;{}") {
		return false
	}
	return strings.HasPrefix(p, "/") || filepath.IsAbs(p)
}

func validAddr(addr string) bool {
	host, port, ok := strings.Cut(addr, ":")
	if !ok || host == "" {
		return false
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return false
	}
	return hostRe.MatchString(host) || host == "127.0.0.1" || host == "localhost"
}
