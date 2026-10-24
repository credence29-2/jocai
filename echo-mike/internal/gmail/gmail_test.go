package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cm "github.com/credence29-2/jocai/echo-mike/internal/mail"
	"github.com/credence29-2/jocai/echo-mike/internal/oauth"
)

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func fake(t *testing.T, h http.HandlerFunc) *Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
	}))
	t.Cleanup(tokens.Close)
	p := oauth.Google("id", "secret")
	p.TokenURL = tokens.URL
	return &Client{Src: oauth.NewSource(p, "refresh", nil, nil), Base: srv.URL}
}

func TestListAndGet(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/gmail/v1/users/me/messages":
			if r.URL.Query().Get("labelIds") != "INBOX" {
				t.Errorf("labelIds = %q", r.URL.Query().Get("labelIds"))
			}
			w.Write([]byte(`{"messages":[{"id":"m1"},{"id":"gone"}],"nextPageToken":"p2"}`))
		case r.URL.Path == "/gmail/v1/users/me/messages/gone":
			w.WriteHeader(404)
			w.Write([]byte(`{"error":{"message":"Not Found"}}`))
		case r.URL.Path == "/gmail/v1/users/me/messages/m1" && r.URL.Query().Get("format") == "metadata":
			w.Write([]byte(`{"id":"m1","threadId":"t1","labelIds":["INBOX","UNREAD"],"snippet":"Hi &amp; bye","internalDate":"1700000000000",
				"payload":{"mimeType":"multipart/mixed","headers":[{"name":"From","value":"=?utf-8?q?Jos=C3=A9?= <jose@x.com>"},{"name":"Subject","value":"Hello"}]}}`))
		case r.URL.Path == "/gmail/v1/users/me/messages/m1":
			full := map[string]any{"id": "m1", "threadId": "t1", "labelIds": []string{"INBOX"}, "internalDate": "1700000000000",
				"payload": map[string]any{"mimeType": "multipart/mixed", "headers": []map[string]string{
					{"name": "From", "value": "Bob <bob@x.com>"}, {"name": "To", "value": "sales@credence.example"},
					{"name": "Delivered-To", "value": "jacob@gmail.com"}, {"name": "Message-ID", "value": "<abc@x.com>"},
					{"name": "Subject", "value": "Quote"}},
					"parts": []any{
						map[string]any{"partId": "0", "mimeType": "multipart/alternative", "parts": []any{
							map[string]any{"partId": "0.0", "mimeType": "text/plain", "body": map[string]any{"data": b64("plain")}},
							map[string]any{"partId": "0.1", "mimeType": "text/html", "headers": []map[string]string{{"name": "Content-Type", "value": "text/html; charset=windows-1252"}},
								"body": map[string]any{"data": base64.RawURLEncoding.EncodeToString([]byte("<p>caf\xe9 \x93hi\x94</p><img src=\"cid:logo\">"))}},
						}},
						map[string]any{"partId": "1", "mimeType": "image/png", "filename": "logo.png", "headers": []map[string]string{{"name": "Content-ID", "value": "<logo>"}, {"name": "Content-Disposition", "value": "inline"}},
							"body": map[string]any{"attachmentId": "att-logo", "size": 10}},
						map[string]any{"partId": "2", "mimeType": "application/pdf", "filename": "quote.pdf", "body": map[string]any{"attachmentId": "att-pdf", "size": 2048}},
					}}}
			json.NewEncoder(w).Encode(full)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	page, err := c.List(context.Background(), cm.Inbox, "", "", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Next != "p2" {
		t.Fatalf("page = %+v", page)
	}
	s := page.Messages[0]
	if s.From.Name != "José" || !s.Unread || s.Snippet != "Hi & bye" || !s.HasAttachments || s.Date.Year() != 2023 {
		t.Fatalf("summary = %+v", s)
	}

	m, err := c.Get(context.Background(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	if m.HTML != "<p>café “hi”</p><img src=\"cid:logo\">" || m.Text != "plain" {
		t.Fatalf("body = %q / %q", m.HTML, m.Text)
	}
	if len(m.Attachments) != 2 || !m.Attachments[0].Inline || m.Attachments[0].ContentID != "logo" || m.Attachments[1].Inline {
		t.Fatalf("attachments = %+v", m.Attachments)
	}
	if m.MessageID != "<abc@x.com>" || len(m.DeliveredTo) != 1 || !m.HasAttachments {
		t.Fatalf("headers = %+v", m)
	}
}

func TestSendUsesAliasAndThread(t *testing.T) {
	var raw, meta string
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/upload/gmail/v1/users/me/messages/send" || r.URL.Query().Get("uploadType") != "multipart" {
			t.Errorf("unexpected %s", r.URL)
		}
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		p, _ := mr.NextPart()
		b, _ := io.ReadAll(p)
		meta = string(b)
		p, _ = mr.NextPart()
		b, _ = io.ReadAll(p)
		raw = string(b)
		w.Write([]byte(`{"id":"sent1"}`))
	})
	err := c.Send(context.Background(), &cm.Outgoing{
		From: cm.Address{Name: "Credence Support", Email: "support@credence.example"},
		To:   []cm.Address{{Email: "a@x.com"}}, Bcc: []cm.Address{{Email: "b@x.com"}},
		Subject: "Hi", HTML: "<p>x</p>", ThreadID: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `From: "Credence Support" <support@credence.example>`) || !strings.Contains(raw, "Bcc: <b@x.com>") {
		t.Fatalf("raw = %s", raw)
	}
	if meta != `{"threadId":"t1"}` {
		t.Fatalf("meta = %s", meta)
	}
}

func TestAliasesAndApply(t *testing.T) {
	var modify []string
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gmail/v1/users/me/settings/sendAs":
			w.Write([]byte(`{"sendAs":[
				{"sendAsEmail":"me@gmail.com","displayName":"Me","isPrimary":true,"isDefault":true,"signature":"<b>Me</b>"},
				{"sendAsEmail":"sales@credence.example","displayName":"Sales","verificationStatus":"accepted","replyToAddress":"help@credence.example"},
				{"sendAsEmail":"pending@credence.example","verificationStatus":"pending"}]}`))
		case "/gmail/v1/users/me/messages/m1/modify":
			b, _ := io.ReadAll(r.Body)
			modify = append(modify, string(b))
			w.Write([]byte(`{}`))
		case "/gmail/v1/users/me/messages/m1/untrash":
			modify = append(modify, "untrash")
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	al, err := c.Aliases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(al) != 2 || al[0].SignatureHTML != "<b>Me</b>" || al[1].ReplyTo != "help@credence.example" {
		t.Fatalf("aliases = %+v", al)
	}
	c.Apply(context.Background(), "m1", cm.MoveArchive)
	c.Apply(context.Background(), "m1", cm.MoveInbox)
	want := []string{`{"addLabelIds":null,"removeLabelIds":["INBOX"]}`, "untrash", `{"addLabelIds":["INBOX"],"removeLabelIds":["SPAM"]}`}
	if strings.Join(modify, "|") != strings.Join(want, "|") {
		t.Fatalf("modify = %v", modify)
	}
}

func TestParseAddrsLenient(t *testing.T) {
	got := ParseAddrs(`"Smith, Ann" <ann@x.com>, broken name <bob@y.com>, undisclosed-recipients:;`)
	if len(got) < 2 || got[0].Email != "ann@x.com" || got[0].Name != "Smith, Ann" || got[1].Email != "bob@y.com" {
		t.Fatalf("got %+v", got)
	}
}
