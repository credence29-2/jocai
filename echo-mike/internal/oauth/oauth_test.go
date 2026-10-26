package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

func TestFlowAndRefresh(t *testing.T) {
	var challenge string
	var refreshes []url.Values
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || r.Form.Get("code") != "the-code" {
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_request"})
				return
			}
			if r.Form.Get("scope") != strings.Join(GraphScopes, " ") {
				t.Errorf("code scope = %q", r.Form.Get("scope"))
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "a1", "refresh_token": "r1", "expires_in": 3600})
		case "refresh_token":
			refreshes = append(refreshes, r.Form)
			if r.Form.Get("refresh_token") == "revoked" {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "smtp-token", "refresh_token": "r2", "expires_in": 3600})
		}
	}))
	defer tokens.Close()

	p := Microsoft("client-1")
	p.TokenURL = tokens.URL
	f, err := Start(p, "me@outlook.com")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.URL)
	q := u.Query()
	challenge = q.Get("code_challenge")
	if !strings.Contains(q.Get("scope"), "SMTP.Send") || q.Get("login_hint") != "me@outlook.com" {
		t.Fatalf("auth url scopes = %q", q.Get("scope"))
	}

	// Play the browser coming back.
	cb := q.Get("redirect_uri") + "?code=the-code&state=" + url.QueryEscape(q.Get("state"))
	cb = strings.Replace(cb, "localhost", "127.0.0.1", 1)
	resp, err := http.Get(cb)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	tok, err := f.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "a1" || tok.RefreshToken != "r1" {
		t.Fatalf("tok = %+v", tok)
	}

	var saved string
	s := NewSource(p, tok.RefreshToken, tok, func(r string) { saved = r })
	if a, _ := s.Token(context.Background(), nil); a != "a1" {
		t.Fatalf("cached token = %s", a)
	}
	a, err := s.Token(context.Background(), SMTPScopes)
	if err != nil || a != "smtp-token" {
		t.Fatalf("smtp token = %s %v", a, err)
	}
	if saved != "r2" || len(refreshes) != 1 || refreshes[0].Get("scope") != strings.Join(SMTPScopes, " ") {
		t.Fatalf("saved=%s refreshes=%v", saved, refreshes)
	}

	bad := NewSource(p, "revoked", nil, nil)
	if _, err := bad.Token(context.Background(), nil); !errors.Is(err, mail.ErrAuth) {
		t.Fatalf("revoked err = %v", err)
	}
}

func TestFlowRejectsWrongState(t *testing.T) {
	p := Google("id", "secret")
	f, err := Start(p, "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.URL)
	if u.Query().Get("access_type") != "offline" {
		t.Fatal("Google needs offline access for a refresh token")
	}
	cb := strings.Replace(u.Query().Get("redirect_uri"), "localhost", "127.0.0.1", 1) + "?code=x&state=forged"
	resp, err := http.Get(cb)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	select {
	case r := <-f.done:
		t.Fatalf("forged state finished the flow: %+v", r)
	default:
	}
	f.Cancel()
	if _, err := f.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestDoRetriesRateLimits(t *testing.T) {
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(403)
			w.Write([]byte(`{"error":{"message":"Quota exceeded for quota metric 'Total Query Cost'"}}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
	}))
	defer tokens.Close()
	p := Google("id", "secret")
	p.TokenURL = tokens.URL
	s := NewSource(p, "r", nil, nil)
	req, _ := http.NewRequest("GET", api.URL, nil)
	var out map[string]bool
	if err := s.Do(req, nil, &out); err != nil || !out["ok"] || calls != 3 {
		t.Fatalf("err=%v calls=%d out=%v", err, calls, out)
	}

	calls = 0
	always := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"message":"rateLimitExceeded"}}`))
	}))
	defer always.Close()
	req, _ = http.NewRequest("GET", always.URL, nil)
	err := s.Do(req, nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.RateLimited || calls != 5 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
