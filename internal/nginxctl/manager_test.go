package nginxctl

import (
	"strings"
	"testing"

	"sabzevar.ir/gateway-core/internal/config"
	"sabzevar.ir/gateway-core/internal/models"
)

func TestRenderShared443(t *testing.T) {
	m := New(config.Config{NginxGatewayAddr: "127.0.0.1:8002"})
	ns := Defaults(config.Config{NginxGatewayAddr: "127.0.0.1:8002", NginxConfPath: "/tmp/gw.conf"})
	ns.Shared443 = true
	ns.TestCmd = "none"
	ns.ReloadCmd = "none"

	gateways := []models.Gateway{
		{
			Name: "نقشه", Host: "map-gateway.sabzevar.ir", Enabled: true, Websocket: true,
			Upstreams: []models.Upstream{{URL: "http://192.168.1.19:7003", Enabled: true}},
		},
		{
			Name: "لاگین", Host: "apisrv-gatewaylogin.sabzevar.ir", Enabled: true,
			Upstreams: []models.Upstream{{URL: "https://apisrv.sabzevar.ir", Enabled: true}},
		},
		{
			Name: "۱۳۷", Host: "apisrv-gateway137.sabzevar.ir", Enabled: true,
			Upstreams: []models.Upstream{{URL: "http://127.0.0.1:13700", Enabled: true}},
		},
	}

	cfg, err := m.Render(ns, gateways, "gateway-admin.sabzevar.ir")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"listen 443 ssl",
		"map-gateway.sabzevar.ir",
		"apisrv-gatewaylogin.sabzevar.ir",
		"apisrv-gateway137.sabzevar.ir",
		"gateway-admin.sabzevar.ir",
		"proxy_pass http://gateway_core",
		"shared 443",
		"/etc/pki/nginx/fullchain.pem",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing %q in:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "listen 8004") || strings.Contains(cfg, "listen 8003") {
		t.Fatalf("shared mode must not use per-domain high ports:\n%s", cfg)
	}
}

func TestRenderShared443CustomSSL(t *testing.T) {
	m := New(config.Config{NginxGatewayAddr: "127.0.0.1:8002"})
	ns := Defaults(config.Config{NginxGatewayAddr: "127.0.0.1:8002"})
	ns.Shared443 = true
	gateways := []models.Gateway{
		{Name: "نقشه", Host: "map-gateway.sabzevar.ir", Enabled: true},
		{
			Name: "sbzl", Host: "sbzl.ir", Enabled: true,
			SSLCert: "/etc/pki/nginx/sbzl.ir/fullchain.pem",
			SSLKey:  "/etc/pki/nginx/sbzl.ir/privkey.pem",
		},
	}
	cfg, err := m.Render(ns, gateways, "gateway-admin.sabzevar.ir")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg, "/etc/pki/nginx/sbzl.ir/fullchain.pem") {
		t.Fatalf("want custom cert:\n%s", cfg)
	}
	if !strings.Contains(cfg, "/etc/pki/nginx/fullchain.pem") {
		t.Fatalf("want default cert still present:\n%s", cfg)
	}
	if !strings.Contains(cfg, "sbzl.ir") || !strings.Contains(cfg, "map-gateway.sabzevar.ir") {
		t.Fatalf("want both hosts:\n%s", cfg)
	}
	if strings.Count(cfg, "listen 443 ssl") < 2 {
		t.Fatalf("want >=2 ssl server blocks for SNI:\n%s", cfg)
	}
}

func TestRenderPerDomainStillWorks(t *testing.T) {
	m := New(config.Config{NginxGatewayAddr: "127.0.0.1:8002"})
	ns := Defaults(config.Config{NginxGatewayAddr: "127.0.0.1:8002"})
	ns.Shared443 = false
	gateways := []models.Gateway{
		{Name: "نقشه", Host: "map-gateway.sabzevar.ir", Enabled: true, ListenPort: 8004},
	}
	cfg, err := m.Render(ns, gateways, "gateway-admin.sabzevar.ir")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg, "listen 8004 ssl") {
		t.Fatalf("want listen 8004:\n%s", cfg)
	}
}
