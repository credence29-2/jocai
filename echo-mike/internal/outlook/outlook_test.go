package outlook

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	cm "github.com/credence29-2/jocai/echo-mike/internal/mail"
	"github.com/credence29-2/jocai/echo-mike/internal/oauth"
)

// fakeSMTP is a tiny SMTP server with STARTTLS and XOAUTH2.
type fakeSMTP struct {
	ln     net.Listener
	tls    *tls.Config
	refuse string // reply to AUTH, e.g. a 535 line
	mu     sync.Mutex
	auth   string
	from   string
	rcpts  []string
	data   string
	sawTLS bool
}

func newFakeSMTP(t *testing.T) (*fakeSMTP, *tls.Config) {
	srv := httptest.NewUnstartedServer(nil)
	srv.StartTLS()
	cert := srv.TLS.Certificates[0]
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	srv.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, tls: &tls.Config{Certificates: []tls.Certificate{cert}}}
	t.Cleanup(func() { ln.Close() })
	go f.serve()
	return f, &tls.Config{RootCAs: pool, ServerName: "example.com"}
}

func (f *fakeSMTP) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(c)
	}
}

func (f *fakeSMTP) handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	w := func(s string) { io.WriteString(c, s+"\r\n") }
	w("220 fake ESMTP")
	inTLS := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch cmd {
		case "EHLO", "HELO":
			if inTLS {
				w("250-fake\r\n250 AUTH XOAUTH2")
			} else {
				w("250-fake\r\n250 STARTTLS")
			}
		case "STARTTLS":
			w("220 go ahead")
			tc := tls.Server(c, f.tls)
			if tc.Handshake() != nil {
				return
			}
			c, r, inTLS = tc, bufio.NewReader(tc), true
			f.mu.Lock()
			f.sawTLS = true
			f.mu.Unlock()
		case "AUTH":
			parts := strings.Fields(line)
			b, _ := base64.StdEncoding.DecodeString(parts[len(parts)-1])
			f.mu.Lock()
			f.auth = string(b)
			f.mu.Unlock()
			if f.refuse != "" {
				w("334 eyJzdGF0dXMiOiI0MDEifQ==")
				r.ReadString('\n')
				w(f.refuse)
				continue
			}
			w("235 ok")
		case "MAIL":
			f.mu.Lock()
			f.from = line
			f.mu.Unlock()
			w("250 ok")
		case "RCPT":
			f.mu.Lock()
			f.rcpts = append(f.rcpts, line)
			f.mu.Unlock()
			w("250 ok")
		case "DATA":
			w("354 go")
			var sb strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil || l == ".\r\n" {
					break
				}
				sb.WriteString(l)
			}
			f.mu.Lock()
			f.data = sb.String()
			f.mu.Unlock()
			w("250 queued")
		case "QUIT":
			w("221 bye")
			return
		default:
			w("250 ok")
		}
	}
}

func newClient(t *testing.T, graph http.HandlerFunc) (*Client, *[]string) {
	var scopes []string
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		scopes = append(scopes, r.Form.Get("scope"))
		tok := "graph-tok"
		if strings.Contains(r.Form.Get("scope"), "SMTP.Send") {
			tok = "smtp-tok"
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": tok, "expires_in": 3600})
	}))
	t.Cleanup(tokens.Close)
	g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer graph-tok" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path != "/v1.0/me/sendMail" && !strings.Contains(strings.Join(r.Header.Values("Prefer"), ","), "ImmutableId") {
			t.Errorf("missing ImmutableId preference on %s", r.URL)
		}
		graph(w, r)
	}))
	t.Cleanup(g.Close)
	p := oauth.Microsoft("client")
	p.TokenURL = tokens.URL
	c := &Client{Src: oauth.NewSource(p, "refresh", nil, nil), Email: "jacob@outlook.com", Base: g.URL + "/v1.0"}
	return c, &scopes
}

func TestSendAliasOverSMTP(t *testing.T) {
	f, tlsCfg := newFakeSMTP(t)
	c, scopes := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("Graph shouldn't be used to send: %s", r.URL)
	})
	c.SMTP = SMTPConfig{Addr: f.ln.Addr().String(), TLS: tlsCfg}
	err := c.Send(context.Background(), &cm.Outgoing{
		From: cm.Address{Name: "Jacob", Email: "jacob.alias@outlook.com"},
		To:   []cm.Address{{Email: "a@x.com"}}, Bcc: []cm.Address{{Email: "secret@x.com"}},
		Subject: "Hi", HTML: "<p>hello</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.sawTLS {
		t.Fatal("sent without TLS")
	}
	if f.auth != "user=jacob@outlook.com\x01auth=Bearer smtp-tok\x01\x01" {
		t.Fatalf("auth = %q", f.auth)
	}
	if f.from != "MAIL FROM:<jacob.alias@outlook.com>" || len(f.rcpts) != 2 {
		t.Fatalf("from=%s rcpts=%v", f.from, f.rcpts)
	}
	if !strings.Contains(f.data, "From: \"Jacob\" <jacob.alias@outlook.com>") || strings.Contains(f.data, "secret@x.com") {
		t.Fatalf("data = %s", f.data)
	}
	if len(*scopes) != 1 || !strings.Contains((*scopes)[0], "SMTP.Send") {
		t.Fatalf("token scopes = %v", *scopes)
	}
}

func TestSMTPDisabledFallsBackForPrimaryOnly(t *testing.T) {
	f, tlsCfg := newFakeSMTP(t)
	f.refuse = "535 5.7.139 Authentication unsuccessful, SmtpClientAuthentication is disabled for the Tenant."
	var sent string
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/me/sendMail" || r.Header.Get("Content-Type") != "text/plain" {
			t.Errorf("unexpected %s", r.URL)
		}
		b, _ := io.ReadAll(r.Body)
		raw, _ := base64.StdEncoding.DecodeString(string(b))
		sent = string(raw)
		w.WriteHeader(202)
	})
	c.SMTP = SMTPConfig{Addr: f.ln.Addr().String(), TLS: tlsCfg}

	alias := &cm.Outgoing{From: cm.Address{Email: "alias@outlook.com"}, To: []cm.Address{{Email: "a@x.com"}}, Subject: "x"}
	if err := c.Send(context.Background(), alias); err == nil || !strings.Contains(err.Error(), "alias@outlook.com") {
		t.Fatalf("alias send should explain the block, got %v", err)
	}
	if sent != "" {
		t.Fatal("alias must not fall back to Graph, which would change the From")
	}
	primary := &cm.Outgoing{From: cm.Address{Email: "Jacob@Outlook.com"}, To: []cm.Address{{Email: "a@x.com"}}, Subject: "x"}
	if err := c.Send(context.Background(), primary); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent, "Subject: x") {
		t.Fatalf("graph got %q", sent)
	}
}

func TestListGetApply(t *testing.T) {
	var calls []string
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/v1.0/me/mailFolders/inbox/messages":
			if r.URL.Query().Get("$orderby") != "receivedDateTime desc" {
				t.Errorf("orderby = %q", r.URL.Query().Get("$orderby"))
			}
			next := "http://" + r.Host + "/v1.0/me/mailFolders/inbox/messages?$skip=30"
			json.NewEncoder(w).Encode(map[string]any{"@odata.nextLink": next, "value": []any{
				map[string]any{"id": "AAA=", "conversationId": "c1", "subject": "Hello", "bodyPreview": "hi\r\n there",
					"receivedDateTime": "2026-10-20T10:00:00Z", "isRead": false, "flag": map[string]string{"flagStatus": "flagged"},
					"from": map[string]any{"emailAddress": map[string]string{"name": "Priya", "address": "priya@x.com"}}},
			}})
		case r.URL.Path == "/v1.0/me/messages" && r.URL.Query().Get("$search") != "":
			if r.URL.Query().Get("$search") != `"toast dealer"` {
				t.Errorf("search = %q", r.URL.Query().Get("$search"))
			}
			w.Write([]byte(`{"value":[]}`))
		case r.URL.Path == "/v1.0/me/messages/AAA=" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{"id": "AAA=", "subject": "Hello", "hasAttachments": true,
				"body":                   map[string]string{"contentType": "html", "content": "<p>Hi</p><img src=\"cid:img1\">"},
				"internetMessageId":      "<m@x.com>",
				"internetMessageHeaders": []map[string]string{{"name": "References", "value": "<r@x.com>"}},
				"toRecipients":           []any{map[string]any{"emailAddress": map[string]string{"address": "jacob.alias@outlook.com"}}}})
		case r.URL.Path == "/v1.0/me/messages/AAA=/attachments":
			w.Write([]byte(`{"value":[{"id":"a1","name":"img.png","contentType":"image/png","size":5,"isInline":true,"contentId":"<img1>"}]}`))
		case r.URL.Path == "/v1.0/me/messages/AAA=/attachments/a1/$value":
			w.Write([]byte("PNG!!"))
		case r.URL.Path == "/v1.0/me/messages/AAA=/move":
			b, _ := io.ReadAll(r.Body)
			calls = append(calls, string(b))
			w.Write([]byte(`{}`))
		case r.URL.Path == "/v1.0/me/messages/AAA=" && r.Method == "PATCH":
			b, _ := io.ReadAll(r.Body)
			calls = append(calls, string(b))
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	})
	page, err := c.List(context.Background(), cm.Inbox, "", "", 30)
	if err != nil {
		t.Fatal(err)
	}
	s := page.Messages[0]
	if !s.Unread || !s.Starred || s.Snippet != "hi there" || s.From.Name != "Priya" || page.Next == "" {
		t.Fatalf("page = %+v", page)
	}
	if _, err := c.List(context.Background(), cm.Inbox, "", "https://evil.example/steal", 30); err == nil {
		t.Fatal("followed a cursor to another host")
	}
	if _, err := c.List(context.Background(), cm.Inbox, "toast dealer", "", 30); err != nil {
		t.Fatal(err)
	}
	m, err := c.Get(context.Background(), "AAA=")
	if err != nil {
		t.Fatal(err)
	}
	if m.HTML == "" || m.References != "<r@x.com>" || len(m.Attachments) != 1 || m.Attachments[0].ContentID != "img1" {
		t.Fatalf("message = %+v", m)
	}
	data, err := c.Attachment(context.Background(), "AAA=", "a1")
	if err != nil || string(data) != "PNG!!" {
		t.Fatalf("attachment = %q %v", data, err)
	}
	c.Apply(context.Background(), "AAA=", cm.MoveArchive)
	c.Apply(context.Background(), "AAA=", cm.Star)
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, `{"destinationId":"archive"}`) || !strings.Contains(joined, `{"flag":{"flagStatus":"flagged"}}`) {
		t.Fatalf("calls = %s", joined)
	}
	_ = url.Values{}
}

func TestSMTPHost(t *testing.T) {
	if smtpFor("me@hotmail.com").Addr != "smtp-mail.outlook.com:587" || smtpFor("me@company.com").Addr != "smtp.office365.com:587" {
		t.Fatal("wrong SMTP host")
	}
}

func TestDrafts(t *testing.T) {
	var calls []string
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(b))
		switch {
		case r.URL.Path == "/v1.0/me/messages/orig/createReply":
			w.Write([]byte(`{"id":"draft1"}`))
		case r.URL.Path == "/v1.0/me/messages/draft1" && r.Method == "PATCH":
			if !strings.Contains(string(b), `"address":"alias@outlook.com"`) || !strings.Contains(string(b), `"subject":"Re: hi"`) {
				t.Errorf("patch body: %s", b)
			}
			w.Write([]byte(`{}`))
		case r.URL.Path == "/v1.0/me/messages/draft1/attachments":
			if !strings.Contains(string(b), `"contentBytes":"aGk="`) {
				t.Errorf("attachment: %s", b)
			}
			w.WriteHeader(201)
			w.Write([]byte(`{}`))
		case r.URL.Path == "/v1.0/me/messages" && r.Method == "POST":
			w.Write([]byte(`{"id":"draft2"}`))
		case r.URL.Path == "/v1.0/me/messages/draft2" && r.Method == "DELETE":
			w.WriteHeader(204)
		case r.URL.Path == "/v1.0/me/mailFolders/drafts/messages":
			w.Write([]byte(`{"value":[{"id":"draft2","subject":"x","isDraft":true,"receivedDateTime":"2026-10-20T10:00:00Z"}]}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	})
	ctx := context.Background()
	id, err := c.SaveDraft(ctx, "", &cm.Outgoing{From: cm.Address{Email: "alias@outlook.com"}, To: []cm.Address{{Email: "a@x.com"}},
		Subject: "Re: hi", HTML: "<p>x</p>", OrigID: "orig", Kind: "reply", Files: []cm.OutFile{{Name: "a.txt", MIMEType: "text/plain", Data: []byte("hi")}}})
	if err != nil || id != "draft1" {
		t.Fatalf("reply draft: %s %v", id, err)
	}
	id, err = c.SaveDraft(ctx, "", &cm.Outgoing{From: cm.Address{Email: "me@outlook.com"}, Subject: "new"})
	if err != nil || id != "draft2" {
		t.Fatalf("new draft: %s %v", id, err)
	}
	page, err := c.List(ctx, cm.Drafts, "", "", 30)
	if err != nil || len(page.Messages) != 1 || page.Messages[0].DraftID != "draft2" {
		t.Fatalf("list: %+v %v", page, err)
	}
	if err := c.DeleteDraft(ctx, "draft2"); err != nil {
		t.Fatal(err)
	}
}

func TestKQL(t *testing.T) {
	got := kql(`printer from:derek subject:"october invoice" has:attachment after:2026-10-01 before:2026-10-20`)
	want := `printer AND from:derek AND subject:october invoice AND hasAttachments:true AND received>=2026-10-01 AND received<2026-10-20`
	if got != want {
		t.Fatalf("got %q", got)
	}
}
