package mail

import (
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestBuildAliasAndAttachment(t *testing.T) {
	m := &Outgoing{
		From:      Address{Name: "Jacob Öyler", Email: "support@credence.example"},
		ReplyTo:   "help@credence.example",
		To:        []Address{{Email: "a@x.com"}},
		Bcc:       []Address{{Email: "hidden@x.com"}},
		Subject:   "Résumé follow-up",
		HTML:      "<p>Hi there</p><div class=\"cm-signature\">Jacob<br>GM</div>",
		Files:     []OutFile{{Name: "notes.txt", MIMEType: "text/plain", Data: []byte("hello file")}},
		InReplyTo: "<abc@x.com>",
	}
	raw, err := Build(m, false, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hidden@x.com") {
		t.Fatal("Bcc leaked into an SMTP message")
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	from, _ := mail.ParseAddress(msg.Header.Get("From"))
	if from.Address != "support@credence.example" || from.Name != "Jacob Öyler" {
		t.Fatalf("From = %+v", from)
	}
	dec := new(mime.WordDecoder)
	if s, _ := dec.DecodeHeader(msg.Header.Get("Subject")); s != "Résumé follow-up" {
		t.Fatalf("Subject = %q", s)
	}
	if msg.Header.Get("Reply-To") != "help@credence.example" || msg.Header.Get("In-Reply-To") != "<abc@x.com>" {
		t.Fatal("missing threading or reply-to headers")
	}
	_, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var parts []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(p)
		parts = append(parts, p.Header.Get("Content-Type"))
		if p.FileName() == "notes.txt" {
			body, _ = base64.StdEncoding.DecodeString(strings.TrimSpace(string(body)))
		}
		if p.FileName() == "notes.txt" && string(body) != "hello file" {
			t.Fatalf("attachment = %q", body)
		}
	}
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "multipart/alternative") {
		t.Fatalf("parts = %v", parts)
	}

	withBcc, _ := Build(m, true, time.Now())
	if !strings.Contains(string(withBcc), "Bcc: <hidden@x.com>") {
		t.Fatal("Gmail copy should carry Bcc")
	}
}

func TestParseAddresses(t *testing.T) {
	got, err := ParseAddresses("a@x.com; Bob <b@y.com>, ")
	if err != nil || len(got) != 2 || got[1].Name != "Bob" {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := ParseAddresses("a@x.com, not an address"); err == nil {
		t.Fatal("expected an error for a typo")
	}
	if got, err := ParseAddresses("  "); err != nil || got != nil {
		t.Fatal("blank should be empty")
	}
}

func TestHTMLToText(t *testing.T) {
	got := HTMLToText("<style>p{}</style><p>Hi&nbsp;Bob,</p><ul><li>one</li><li>two</li></ul><br><br><br>Thanks")
	want := "Hi Bob,\n- one\n- two\n\nThanks\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

var ids = []Identity{
	{ID: "g1", AccountID: "gmail", Email: "jacob@gmail.com", Default: true},
	{ID: "g2", AccountID: "gmail", Email: "sales@credence.example"},
	{ID: "o1", AccountID: "outlook", Email: "jacob@outlook.com", Default: true},
	{ID: "o2", AccountID: "outlook", Email: "jacob.alias@outlook.com"},
}

func TestPickIdentityPrefersAliasItWasSentTo(t *testing.T) {
	cases := []struct {
		acct string
		to   []Address
		cc   []Address
		want string
	}{
		{"gmail", []Address{{Email: "Sales@Credence.example"}}, nil, "g2"},
		{"gmail", []Address{{Email: "someone@else.com"}}, []Address{{Email: "sales+leads@credence.example"}}, "g2"},
		{"gmail", []Address{{Email: "list@else.com"}}, nil, "g1"},
		{"outlook", []Address{{Email: "jacob.alias@outlook.com"}}, nil, "o2"},
		// Delivered to an alias that belongs to another account.
		{"outlook", []Address{{Email: "sales@credence.example"}}, nil, "g2"},
	}
	for _, c := range cases {
		got, ok := PickIdentity(ids, c.acct, &Message{Summary: Summary{To: c.to}, Cc: c.cc})
		if !ok || got.ID != c.want {
			t.Errorf("%v: got %s want %s", c.to, got.ID, c.want)
		}
	}
	got, _ := PickIdentity(ids, "gmail", &Message{DeliveredTo: []Address{{Email: "sales@credence.example"}}})
	if got.ID != "g2" {
		t.Errorf("Delivered-To match: got %s", got.ID)
	}
}

func TestMakeDraftReplyAll(t *testing.T) {
	msg := &Message{
		Summary: Summary{
			From:    Address{Name: "Client", Email: "client@shop.com"},
			To:      []Address{{Email: "sales@credence.example"}, {Email: "boss@credence.example"}},
			Subject: "RE: Order",
		},
		Cc:        []Address{{Email: "jacob@gmail.com"}, {Email: "client@shop.com"}, {Email: "ops@shop.com"}},
		MessageID: "<m1@shop.com>",
		Text:      "Hello <there>",
	}
	d := MakeDraft(ReplyAll, ids, "gmail", msg)
	if d.IdentityID != "g2" {
		t.Fatalf("from = %s", d.IdentityID)
	}
	if d.Subject != "RE: Order" {
		t.Fatalf("subject = %q", d.Subject)
	}
	if len(d.To) != 1 || d.To[0].Email != "client@shop.com" {
		t.Fatalf("to = %v", d.To)
	}
	var cc []string
	for _, a := range d.Cc {
		cc = append(cc, a.Email)
	}
	if strings.Join(cc, ",") != "boss@credence.example,ops@shop.com" {
		t.Fatalf("cc = %v", cc)
	}
	if d.InReplyTo != "<m1@shop.com>" || !strings.Contains(d.QuoteHTML, "Hello &lt;there&gt;") {
		t.Fatal("bad threading or quote escaping")
	}

	f := MakeDraft(Forward, ids, "gmail", &Message{Summary: Summary{Subject: "Fw: x"}, Attachments: []Attachment{{ID: "a"}, {ID: "b", Inline: true}}})
	if f.Subject != "Fw: x" || len(f.Attachments) != 1 || len(f.To) != 0 {
		t.Fatalf("forward = %+v", f)
	}
	if MakeDraft(Reply, ids, "gmail", &Message{Summary: Summary{Subject: "Order"}}).Subject != "Re: Order" {
		t.Fatal("missing Re:")
	}
}

func TestReplyToOwnSentMessage(t *testing.T) {
	msg := &Message{Summary: Summary{
		From: Address{Email: "sales@credence.example"},
		To:   []Address{{Email: "client@shop.com"}},
	}}
	d := MakeDraft(Reply, ids, "gmail", msg)
	if len(d.To) != 1 || d.To[0].Email != "client@shop.com" {
		t.Fatalf("to = %v", d.To)
	}
}
