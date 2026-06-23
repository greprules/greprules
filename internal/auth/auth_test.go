package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreAndRequireToken(t *testing.T) {
	t.Setenv("GREPRULES_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	expiresAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if err := StoreToken("https://api.greprules.io/", "grcli_test-token", expiresAt); err != nil {
		t.Fatal(err)
	}
	token, err := RequiredToken("https://api.greprules.io")
	if err != nil {
		t.Fatal(err)
	}
	if token != "grcli_test-token" {
		t.Fatalf("token = %q", token)
	}
}

func TestRunLoginStoresBrowserApprovedToken(t *testing.T) {
	t.Setenv("GREPRULES_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	var sawStart, sawPoll bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/api/cli-auth/start":
			sawStart = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success":                   true,
				"device_code":               "device-code-for-test",
				"user_code":                 "ABCD-2345",
				"verification_uri":          "https://greprules.test/cli-auth",
				"verification_uri_complete": "https://greprules.test/cli-auth?code=ABCD-2345",
				"expires_in":                60,
				"interval":                  1,
			})
		case "/api/cli-auth/poll":
			sawPoll = true
			var request struct {
				DeviceCode string `json:"device_code"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.DeviceCode != "device-code-for-test" {
				t.Fatalf("device code = %q", request.DeviceCode)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success":    true,
				"status":     "approved",
				"token":      "grcli_browser-approved-token",
				"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := RunCommand(context.Background(), []string{"login", "--registry", server.URL, "--no-browser"}); err != nil {
		t.Fatal(err)
	}
	token, err := RequiredToken(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if token != "grcli_browser-approved-token" {
		t.Fatalf("token = %q", token)
	}
	if !sawStart || !sawPoll {
		t.Fatalf("expected start and poll, saw start=%t poll=%t", sawStart, sawPoll)
	}
}

func TestRequiredTokenRejectsExpiredToken(t *testing.T) {
	t.Setenv("GREPRULES_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	expiresAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if err := StoreToken("https://api.greprules.io", "grcli_expired-token", expiresAt); err != nil {
		t.Fatal(err)
	}
	_, err := RequiredToken("https://api.greprules.io")
	if err == nil || !strings.Contains(err.Error(), "login expired") {
		t.Fatalf("expected expired login error, got %v", err)
	}
}
