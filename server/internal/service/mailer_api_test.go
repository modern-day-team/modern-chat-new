package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modern-day-team/modern-chat-new/server/internal/config"
)

func TestSendAPIMail(t *testing.T) {
	var got apiMailRequest
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.SMTPConfig{
		Provider: "api",
		APIURL:   srv.URL,
		APIKey:   "test-key",
		From:     "noreply@example.com",
	}
	if err := sendAPIMail(cfg, "to@example.com", "Subject", "Body"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if got.To != "to@example.com" || got.Subject != "Subject" || got.Text != "Body" || got.From != "noreply@example.com" {
		t.Errorf("payload = %+v", got)
	}
}

func TestSendAPIMailError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid api key"}`))
	}))
	defer srv.Close()
	cfg := config.SMTPConfig{Provider: "api", APIURL: srv.URL, APIKey: "bad", From: "a@b.c"}
	err := sendAPIMail(cfg, "to@example.com", "s", "b")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !contains(err.Error(), "401") || !contains(err.Error(), "invalid api key") {
		t.Errorf("error should include status and body: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestMailConfigEnabledByProvider(t *testing.T) {
	if !(config.SMTPConfig{Provider: "api", APIURL: "https://x", APIKey: "k"}).Enabled() {
		t.Error("api config should be enabled")
	}
	if (config.SMTPConfig{Provider: "api", APIURL: "https://x"}).Enabled() {
		t.Error("api config without key must be disabled")
	}
	if !(config.SMTPConfig{Provider: "smtp", Host: "smtp.x", From: "a@b.c"}).Enabled() {
		t.Error("smtp config should be enabled")
	}
}
