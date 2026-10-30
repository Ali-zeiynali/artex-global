package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func TestAccountsProxyReplacesBrowserAuthorizationAndHidesCookies(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/accounts/telegram/verify" || r.URL.RawQuery != "" {
			t.Errorf("unexpected target: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer private-bridge" || r.Header.Get("Cookie") != "" {
			t.Error("browser credentials reached host service")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"code":"12345"}` {
			t.Error("request body changed")
		}
		w.Header().Set("Set-Cookie", "private=not-for-browser")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"step":"password"}`))
	}))
	defer upstream.Close()
	t.Setenv("ARTEX_ACCOUNTS_URL", upstream.URL)
	t.Setenv("ARTEX_ACCOUNTS_TOKEN", "private-bridge")
	r := httptest.NewRequest("POST", "/api/accounts/telegram/verify?token=must-not-forward", strings.NewReader(`{"code":"12345"}`))
	r.Header.Set("Authorization", "Bearer browser-token")
	r.Header.Set("Cookie", "artex_token=browser-token")
	w := httptest.NewRecorder()
	(&Server{}).accountsProxy(w, r)
	if w.Code != 200 || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Header())
	}
}

func TestAccountsProxyUnavailableHasNoConfigurationSecrets(t *testing.T) {
	t.Setenv("ARTEX_ACCOUNTS_URL", "")
	t.Setenv("ARTEX_ACCOUNTS_TOKEN", "secret-must-not-leak")
	w := httptest.NewRecorder()
	(&Server{}).accountsProxy(w, httptest.NewRequest("GET", "/api/accounts", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "secret-must-not-leak") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestMCPAccountCredentialInventoryReturnsPresenceOnly(t *testing.T) {
	fields := mcpCredentialFields(&db.MCPServer{Name: "example", Env: json.RawMessage(`{"Authorization":"Bearer private-value","API_KEY":"","NORMAL_ENV":"not-a-credential"}`)})
	encoded, _ := json.Marshal(fields)
	if len(fields) != 2 || strings.Contains(string(encoded), "private-value") || strings.Contains(string(encoded), "not-a-credential") {
		t.Fatal(string(encoded))
	}
	if credentialPresent("Bearer ") || !credentialPresent("Bearer actual-key") {
		t.Fatal("credential presence classification failed")
	}
}
