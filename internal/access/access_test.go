package access

import (
	"net/http"
	"testing"

	"sabzevar.ir/gateway-core/internal/models"
)

func TestOpenWhenUnrestricted(t *testing.T) {
	r := req("GET", "", "", "")
	if !Allowed(models.Gateway{}, r) {
		t.Fatal("open gateway should allow")
	}
}

func TestOriginAllow(t *testing.T) {
	gw := models.Gateway{AllowedOrigins: []string{"map.sabzevar.ir", "*.city.ir"}}
	if !Allowed(gw, req("GET", "https://map.sabzevar.ir", "", "")) {
		t.Fatal("exact origin")
	}
	if !Allowed(gw, req("GET", "https://app.city.ir", "", "")) {
		t.Fatal("wildcard origin")
	}
	if Allowed(gw, req("GET", "https://evil.ir", "", "")) {
		t.Fatal("foreign origin")
	}
	if Allowed(gw, req("GET", "", "", "")) {
		t.Fatal("missing origin")
	}
	if !Allowed(gw, req("GET", "", "https://map.sabzevar.ir/page", "")) {
		t.Fatal("referer fallback")
	}
}

func TestTokenAllow(t *testing.T) {
	gw := models.Gateway{AccessTokens: []models.AccessToken{
		{Name: "android", Token: "secret-token-1", Enabled: true},
		{Name: "off", Token: "dead", Enabled: false},
	}}
	if !Allowed(gw, req("GET", "", "", "secret-token-1")) {
		t.Fatal("header token")
	}
	if Allowed(gw, req("GET", "", "", "dead")) {
		t.Fatal("disabled token")
	}
	if Allowed(gw, req("GET", "https://map.sabzevar.ir", "", "")) {
		t.Fatal("origin should not bypass token-only gateway")
	}
}

func TestOriginOrToken(t *testing.T) {
	gw := models.Gateway{
		AllowedOrigins: []string{"web.sabzevar.ir"},
		AccessTokens:   []models.AccessToken{{Name: "app", Token: "tok", Enabled: true}},
	}
	if !Allowed(gw, req("GET", "https://web.sabzevar.ir", "", "")) {
		t.Fatal("web origin")
	}
	if !Allowed(gw, req("GET", "", "", "tok")) {
		t.Fatal("app token")
	}
	if Allowed(gw, req("GET", "https://other.ir", "", "nope")) {
		t.Fatal("neither")
	}
}

func req(method, origin, referer, token string) *http.Request {
	r, _ := http.NewRequest(method, "https://map-gateway.sabzevar.ir/x", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if referer != "" {
		r.Header.Set("Referer", referer)
	}
	if token != "" {
		r.Header.Set("X-Gateway-Token", token)
	}
	return r
}
