// Package oauth signs accounts in through the system browser with the OAuth
// 2.0 authorization code flow and PKCE, using a one-off loopback listener for
// the redirect, and keeps access tokens fresh.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/credence29-2/jocai/cpc-mail/internal/mail"
)

// Provider describes one sign-in service.
type Provider struct {
	Name         string
	AuthURL      string
	TokenURL     string
	ClientID     string
	ClientSecret string
	// Scopes are asked for at sign-in.
	Scopes []string
	// CodeScopes, when set, are what the first token is issued for.
	// Microsoft needs this because one token can only cover one API, while
	// consent can cover several.
	CodeScopes []string
	Extra      map[string]string
}

// Google returns the Gmail sign-in. gmail.modify covers reading, labels,
// trash, sending, and listing send-as aliases with their signatures.
func Google(clientID, clientSecret string) *Provider {
	return &Provider{
		Name:         "Google",
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{"https://www.googleapis.com/auth/gmail.modify"},
		// offline + consent makes Google hand back a refresh token every
		// time, even for an account that was connected before.
		Extra: map[string]string{"access_type": "offline", "prompt": "consent"},
	}
}

// Microsoft scopes. Graph reads and organizes mail. SMTP sends it, because
// Graph always sends from the primary address and ignores aliases.
var (
	GraphScopes = []string{"offline_access", "https://graph.microsoft.com/User.Read", "https://graph.microsoft.com/Mail.ReadWrite", "https://graph.microsoft.com/Mail.Send"}
	SMTPScopes  = []string{"offline_access", "https://outlook.office.com/SMTP.Send"}
)

// Microsoft returns the Outlook.com and Microsoft 365 sign-in.
func Microsoft(clientID string) *Provider {
	return &Provider{
		Name:       "Microsoft",
		AuthURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenURL:   "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		ClientID:   clientID,
		Scopes:     append(append([]string{}, GraphScopes...), SMTPScopes[1:]...),
		CodeScopes: GraphScopes,
		Extra:      map[string]string{"prompt": "select_account"},
	}
}

// Token is what the token endpoint returns.
type Token struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// Flow is one sign-in in progress.
type Flow struct {
	URL  string
	done chan result
	stop func()
}

type result struct {
	tok *Token
	err error
}

// Start opens a loopback listener and returns the URL to show in the
// browser. Wait for the result with Wait.
func Start(p *Provider, loginHint string) (*Flow, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	// Browsers may try ::1 first for "localhost", so listen there too when
	// the port is free.
	ln6, _ := net.Listen("tcp", "[::1]:"+strconv.Itoa(port))

	// No trailing slash, so it matches "http://localhost" as registered with
	// Microsoft (which ignores the port for loopback addresses).
	redirect := fmt.Sprintf("http://localhost:%d", port)
	verifier := randString(48)
	state := randString(24)
	sum := sha256.Sum256([]byte(verifier))

	q := url.Values{
		"client_id":             {p.ClientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirect},
		"scope":                 {strings.Join(p.Scopes, " ")},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	for k, v := range p.Extra {
		q.Set(k, v)
	}
	if loginHint != "" {
		q.Set("login_hint", loginHint)
	}

	f := &Flow{URL: p.AuthURL + "?" + q.Encode(), done: make(chan result, 1)}
	var once sync.Once
	finish := func(r result) { once.Do(func() { f.done <- r }) }

	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		v := r.URL.Query()
		if v.Get("state") != state {
			page(w, "That sign-in link is out of date. Go back to CPC Mail and try again.")
			return
		}
		if e := v.Get("error"); e != "" {
			msg := v.Get("error_description")
			if msg == "" {
				msg = e
			}
			page(w, "Sign-in was cancelled or refused: "+msg)
			finish(result{err: fmt.Errorf("%s sign-in: %s", p.Name, msg)})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		form := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {v.Get("code")},
			"redirect_uri":  {redirect},
			"code_verifier": {verifier},
		}
		if len(p.CodeScopes) > 0 {
			form.Set("scope", strings.Join(p.CodeScopes, " "))
		}
		tok, err := p.post(ctx, form)
		if err != nil {
			page(w, "Couldn't finish signing in: "+err.Error())
			finish(result{err: err})
			return
		}
		page(w, "You're signed in. You can close this tab and go back to CPC Mail.")
		finish(result{tok: tok})
	})
	go srv.Serve(ln)
	if ln6 != nil {
		go srv.Serve(ln6)
	}
	f.stop = func() { srv.Close() }
	return f, nil
}

// Wait blocks until the browser comes back, ctx ends, or 10 minutes pass.
func (f *Flow) Wait(ctx context.Context) (*Token, error) {
	defer f.stop()
	select {
	case r := <-f.done:
		// Let the browser receive its page before the server closes.
		time.Sleep(200 * time.Millisecond)
		return r.tok, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Minute):
		return nil, errors.New("sign-in timed out")
	}
}

// Cancel abandons the flow.
func (f *Flow) Cancel() {
	select {
	case f.done <- result{err: context.Canceled}:
	default:
	}
}

func page(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>CPC Mail</title>
<body style="font:16px system-ui,sans-serif;display:grid;place-items:center;height:90vh;color:#222">
<p style="max-width:480px;text-align:center">%s</p></body>`, html.EscapeString(msg))
}

// Refresh trades a refresh token for an access token covering scopes.
func (p *Provider) Refresh(ctx context.Context, refresh string, scopes []string) (*Token, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}
	if len(scopes) > 0 {
		form.Set("scope", strings.Join(scopes, " "))
	}
	tok, err := p.post(ctx, form)
	if err != nil {
		return nil, err
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refresh
	}
	return tok, nil
}

// HTTPClient is used for token requests. Tests replace it.
var HTTPClient = &http.Client{Timeout: 30 * time.Second}

func (p *Provider) post(ctx context.Context, form url.Values) (*Token, error) {
	form.Set("client_id", p.ClientID)
	if p.ClientSecret != "" {
		form.Set("client_secret", p.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%s token endpoint: %s", p.Name, resp.Status)
	}
	if body.Error != "" || body.AccessToken == "" {
		if body.Error == "invalid_grant" {
			return nil, mail.ErrAuth
		}
		msg := body.Description
		if msg == "" {
			msg = body.Error
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("%s sign-in: %s", p.Name, firstLine(msg))
	}
	exp := body.ExpiresIn
	if exp <= 0 {
		exp = 3600
	}
	return &Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(exp)*time.Second - time.Minute),
	}, nil
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i > 0 {
		return s[:i]
	}
	return s
}

func randString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Source hands out access tokens for one account, refreshing as needed and
// reporting a rotated refresh token through Save.
type Source struct {
	P    *Provider
	Save func(refresh string)

	mu      sync.Mutex
	refresh string
	cache   map[string]*Token
}

// NewSource starts a Source from a stored refresh token. first, if not nil,
// is a fresh access token for the provider's CodeScopes (or Scopes).
func NewSource(p *Provider, refresh string, first *Token, save func(string)) *Source {
	s := &Source{P: p, Save: save, refresh: refresh, cache: map[string]*Token{}}
	if first != nil {
		s.cache[s.key(nil)] = first
	}
	return s
}

func (s *Source) key(scopes []string) string {
	if scopes == nil {
		scopes = s.P.CodeScopes
		if scopes == nil {
			scopes = s.P.Scopes
		}
	}
	return strings.Join(scopes, " ")
}

// Token returns a valid access token for scopes, or for the default set when
// scopes is nil.
func (s *Source) Token(ctx context.Context, scopes []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(scopes)
	if t := s.cache[k]; t != nil && time.Now().Before(t.Expiry) {
		return t.AccessToken, nil
	}
	if scopes == nil && s.P.CodeScopes != nil {
		scopes = s.P.CodeScopes
	}
	t, err := s.P.Refresh(ctx, s.refresh, scopes)
	if err != nil {
		return "", err
	}
	s.cache[k] = t
	if t.RefreshToken != s.refresh {
		s.refresh = t.RefreshToken
		if s.Save != nil {
			s.Save(t.RefreshToken)
		}
	}
	return t.AccessToken, nil
}

// Invalidate drops cached access tokens, after the API rejected one.
func (s *Source) Invalidate() {
	s.mu.Lock()
	s.cache = map[string]*Token{}
	s.mu.Unlock()
}

// APIError is a failed API call.
type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string { return e.Msg }

// Do sends req with a bearer token for scopes (nil for the default set) and
// decodes a JSON reply into out when out isn't nil. A 401 gets one retry with
// a fresh token, then becomes mail.ErrAuth. A 429 or 5xx gets one retry.
func (s *Source) Do(req *http.Request, scopes []string, out any) error {
	ctx := req.Context()
	for attempt := 0; ; attempt++ {
		tok, err := s.Token(ctx, scopes)
		if err != nil {
			return err
		}
		if attempt > 0 && req.GetBody != nil {
			req.Body, _ = req.GetBody()
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := HTTPClient.Do(req)
		if err != nil {
			return err
		}
		if attempt == 0 && (resp.StatusCode == 401 || resp.StatusCode == 429 || resp.StatusCode >= 500) {
			resp.Body.Close()
			if resp.StatusCode == 401 {
				s.Invalidate()
			} else {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second):
				}
			}
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == 401 {
			return mail.ErrAuth
		}
		if resp.StatusCode >= 300 {
			var e struct {
				Error json.RawMessage `json:"error"`
			}
			json.NewDecoder(resp.Body).Decode(&e)
			return &APIError{Status: resp.StatusCode, Msg: fmt.Sprintf("%s: %s", resp.Status, errText(e.Error))}
		}
		if out == nil || resp.StatusCode == 204 {
			return nil
		}
		if raw, ok := out.(*[]byte); ok {
			*raw, err = io.ReadAll(resp.Body)
			return err
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
}

// errText pulls the message out of Google's and Microsoft's error shapes.
func errText(raw json.RawMessage) string {
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	return "request failed"
}
