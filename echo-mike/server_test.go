package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/credence29-2/jocai/echo-mike/internal/demo"
	"github.com/credence29-2/jocai/echo-mike/internal/mail"
	"github.com/credence29-2/jocai/echo-mike/internal/store"
)

type harness struct {
	t      *testing.T
	a      *app
	h      http.Handler
	opened []string
}

func newHarness(t *testing.T) *harness {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hs := &harness{t: t}
	hs.a = newApp(st, true, func(u string) error { hs.opened = append(hs.opened, u); return nil })
	hs.h = hs.a.routes()
	return hs
}

func (hs *harness) call(method, path string, body any, out any) int {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:5555"+path, rd)
	req.Header.Set("X-Echo-Token", hs.a.token)
	rec := httptest.NewRecorder()
	hs.h.ServeHTTP(rec, req)
	if out != nil {
		json.Unmarshal(rec.Body.Bytes(), out)
	}
	return rec.Code
}

func TestTokenAndHostGuard(t *testing.T) {
	hs := newHarness(t)
	for _, c := range []struct {
		host, token string
		want        int
	}{
		{"127.0.0.1:5555", "", 403},
		{"127.0.0.1:5555", "wrong", 403},
		{"evil.example:5555", hs.a.token, 403},
		{"127.0.0.1:5555", hs.a.token, 200},
	} {
		req := httptest.NewRequest("GET", "http://"+c.host+"/api/state", nil)
		req.Host = c.host
		if c.token != "" {
			req.Header.Set("X-Echo-Token", c.token)
		}
		rec := httptest.NewRecorder()
		hs.h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s %q: got %d want %d", c.host, c.token, rec.Code, c.want)
		}
	}
}

func TestUnifiedListAndReplyFromAlias(t *testing.T) {
	hs := newHarness(t)
	var list listOut
	if code := hs.call("POST", "/api/list", listIn{Folder: mail.Inbox}, &list); code != 200 {
		t.Fatalf("list code %d", code)
	}
	accts := map[string]bool{}
	for i, m := range list.Messages {
		accts[m.AccountID] = true
		if i > 0 && m.Date.After(list.Messages[i-1].Date) {
			t.Fatal("list not sorted newest first")
		}
	}
	if len(accts) != 2 {
		t.Fatalf("expected messages from both accounts, got %v", accts)
	}

	// Maria wrote to support@, an alias on the Gmail account.
	var maria mail.Summary
	for _, m := range list.Messages {
		if strings.Contains(m.Subject, "Terminal 3") {
			maria = m
		}
	}
	var d mail.Draft
	hs.call("POST", "/api/draft", map[string]string{"accountId": maria.AccountID, "id": maria.ID, "kind": "reply"}, &d)
	var st stateOut
	hs.call("GET", "/api/state", nil, &st)
	var from mail.Identity
	for _, i := range st.Identities {
		if i.ID == d.IdentityID {
			from = i
		}
	}
	if from.Email != "support@credence.example" {
		t.Fatalf("reply from = %+v", from)
	}

	var res map[string]any
	code := hs.call("POST", "/api/send", sendIn{
		IdentityID: d.IdentityID, To: "maria@tacoshack.example", Subject: d.Subject, HTML: "<p>On it</p>",
		InReplyTo: d.InReplyTo, ThreadID: d.ThreadID, ReplyAccountID: maria.AccountID,
	}, &res)
	if code != 200 {
		t.Fatalf("send: %d %v", code, res)
	}
	box := hs.a.providers["demo-g"].(*demo.Box)
	sent := box.SentLog[len(box.SentLog)-1]
	if sent.From.Email != "support@credence.example" || sent.ReplyTo != "help@credence.example" || sent.ThreadID != d.ThreadID {
		t.Fatalf("sent = %+v", sent)
	}

	// A bad address is an error, not a silent drop.
	code = hs.call("POST", "/api/send", sendIn{IdentityID: d.IdentityID, To: "maria@tacoshack.example, oops"}, &res)
	if code != 500 || !strings.Contains(res["error"].(string), "oops") {
		t.Fatalf("bad address: %d %v", code, res)
	}
}

func TestIdentityDefaultsAndImport(t *testing.T) {
	hs := newHarness(t)
	var st stateOut
	hs.call("GET", "/api/state", nil, &st)
	count := len(st.Identities)

	var added mail.Identity
	hs.call("POST", "/api/identity", mail.Identity{AccountID: "demo-o", Email: "jacob.alias@outlook.example", Name: "J", Default: true, SignatureHTML: "<i>J</i>"}, &added)
	hs.call("GET", "/api/state", nil, &st)
	defaults := 0
	for _, i := range st.Identities {
		if i.AccountID == "demo-o" && i.Default {
			defaults++
			if i.ID != added.ID {
				t.Fatal("new default didn't replace the old one")
			}
		}
	}
	if defaults != 1 || len(st.Identities) != count+1 {
		t.Fatalf("defaults=%d identities=%d", defaults, len(st.Identities))
	}

	// Importing again adds nothing and keeps edited signatures.
	var out map[string]int
	hs.call("POST", "/api/aliases/import", map[string]string{"accountId": "demo-g"}, &out)
	if out["added"] != 0 {
		t.Fatalf("re-import added %d", out["added"])
	}
	if code := hs.call("POST", "/api/identity", mail.Identity{AccountID: "demo-o", Email: "not an email"}, nil); code == 200 {
		t.Fatal("accepted a bad address")
	}

	// The last address on an account can't be deleted.
	hs.call("GET", "/api/state", nil, &st)
	var res map[string]any
	for _, i := range st.Identities {
		if i.AccountID == "demo-o" {
			hs.call("POST", "/api/identity/delete", map[string]string{"id": i.ID}, &res)
		}
	}
	if res["error"] == nil {
		t.Fatal("deleted the last From address")
	}
}

func TestOpenOnlyWebLinks(t *testing.T) {
	hs := newHarness(t)
	for _, u := range []string{"https://example.com/a", "javascript:alert(1)", "file:///C:/Windows", "mailto:a@x.com"} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:5555/open?t="+hs.a.token+"&u="+u, nil)
		rec := httptest.NewRecorder()
		hs.h.ServeHTTP(rec, req)
		if rec.Code != 204 {
			t.Fatalf("%s: %d", u, rec.Code)
		}
	}
	if strings.Join(hs.opened, " ") != "https://example.com/a mailto:a@x.com" {
		t.Fatalf("opened %v", hs.opened)
	}
}

func TestAttachmentDownloadIsNotRendered(t *testing.T) {
	hs := newHarness(t)
	req := httptest.NewRequest("GET", "http://127.0.0.1:5555/api/attachment?t="+hs.a.token+"&a=demo-g&m=3&att=f1&name=x.html&type=text/html&inline=1", nil)
	rec := httptest.NewRecorder()
	hs.h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("code=%d headers=%v", rec.Code, rec.Header())
	}
}
