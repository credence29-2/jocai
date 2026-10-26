package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"regexp"
	"strings"
	"time"
)

// Build renders m as an RFC 5322 message. Gmail's API wants the Bcc header in
// the message and strips it before delivery; SMTP must never see it, so the
// caller says which.
func Build(m *Outgoing, withBcc bool, now time.Time) ([]byte, error) {
	if m.From.Email == "" {
		return nil, fmt.Errorf("no From address")
	}
	if len(m.To)+len(m.Cc)+len(m.Bcc) == 0 {
		return nil, fmt.Errorf("add at least one recipient")
	}
	var b bytes.Buffer
	h := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	h("From", formatAddr(m.From))
	h("To", formatList(m.To))
	h("Cc", formatList(m.Cc))
	if withBcc {
		h("Bcc", formatList(m.Bcc))
	}
	h("Reply-To", m.ReplyTo)
	h("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	h("Date", now.Format(time.RFC1123Z))
	h("Message-ID", newMessageID(m.From.Email))
	h("In-Reply-To", m.InReplyTo)
	h("References", m.References)
	h("MIME-Version", "1.0")

	alt, altType, err := alternative(m.HTML, m.ICS, m.ICSMethod)
	if err != nil {
		return nil, err
	}
	if len(m.Files) == 0 {
		h("Content-Type", altType)
		b.WriteString("\r\n")
		b.Write(alt)
		return b.Bytes(), nil
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	b.WriteString("\r\n")
	part, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {altType}})
	if err != nil {
		return nil, err
	}
	part.Write(alt)
	for _, f := range m.Files {
		typ := f.MIMEType
		if typ == "" {
			typ = "application/octet-stream"
		}
		name := sanitizeFilename(f.Name)
		part, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {mime.FormatMediaType(typ, map[string]string{"name": name})},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": name})},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			return nil, err
		}
		writeBase64Lines(part, f.Data)
	}
	mw.Close()
	b.Write(body.Bytes())
	return b.Bytes(), nil
}

// alternative builds the text and HTML versions of the body.
func alternative(htmlBody, ics, icsMethod string) ([]byte, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	parts := []struct{ typ, body string }{
		{"text/plain; charset=utf-8", HTMLToText(htmlBody)},
		{"text/html; charset=utf-8", "<html><body>" + htmlBody + "</body></html>"},
	}
	if ics != "" {
		// Calendar servers look for this part to update the organizer's
		// attendee list.
		parts = append(parts, struct{ typ, body string }{"text/calendar; charset=utf-8; method=" + icsMethod, ics})
	}
	for _, p := range parts {
		w, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {p.typ},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, "", err
		}
		qp := quotedprintable.NewWriter(w)
		qp.Write([]byte(p.body))
		qp.Close()
	}
	mw.Close()
	return buf.Bytes(), "multipart/alternative; boundary=" + mw.Boundary(), nil
}

func writeBase64Lines(w interface{ Write([]byte) (int, error) }, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		w.Write([]byte(enc[:76] + "\r\n"))
		enc = enc[76:]
	}
	if enc != "" {
		w.Write([]byte(enc + "\r\n"))
	}
}

func formatAddr(a Address) string {
	return (&mail.Address{Name: a.Name, Address: a.Email}).String()
}

func formatList(list []Address) string {
	parts := make([]string, 0, len(list))
	for _, a := range list {
		parts = append(parts, formatAddr(a))
	}
	return strings.Join(parts, ", ")
}

func newMessageID(from string) string {
	domain := "localhost"
	if i := strings.LastIndexByte(from, '@'); i >= 0 && i < len(from)-1 {
		domain = from[i+1:]
	}
	r := make([]byte, 12)
	rand.Read(r)
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), hex.EncodeToString(r), domain)
}

func sanitizeFilename(n string) string {
	n = strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == '\\' || r == '/' {
			return '_'
		}
		return r
	}, n)
	if n == "" {
		return "attachment"
	}
	return n
}

// ParseAddresses reads a comma or semicolon separated list the way people
// type it: "a@x.com; Bob <b@y.com>". Anything that isn't an address is an
// error so a typo doesn't silently drop a recipient.
func ParseAddresses(s string) ([]Address, error) {
	s = strings.ReplaceAll(s, ";", ",")
	if strings.TrimSpace(strings.Trim(s, ", ")) == "" {
		return nil, nil
	}
	list, err := mail.ParseAddressList(s)
	if err != nil {
		// Retry piece by piece so the error names the bad entry.
		var out []Address
		for _, p := range strings.Split(s, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			a, err := mail.ParseAddress(p)
			if err != nil {
				return nil, fmt.Errorf("%q isn't a valid email address", p)
			}
			out = append(out, Address{Name: a.Name, Email: a.Address})
		}
		return out, nil
	}
	out := make([]Address, 0, len(list))
	for _, a := range list {
		out = append(out, Address{Name: a.Name, Email: a.Address})
	}
	return out, nil
}

var (
	reBlockEnd = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|li|tr|h[1-6]|blockquote)>`)
	reLi       = regexp.MustCompile(`(?i)<li[^>]*>`)
	reDrop     = regexp.MustCompile(`(?is)<(style|script|head)[^>]*>.*?</(style|script|head)>`)
	reCell     = regexp.MustCompile(`(?i)</t[dh]>`)
	reTag      = regexp.MustCompile(`(?s)<[^>]*>`)
	reBlank    = regexp.MustCompile(`\n{3,}`)
	reSpaces   = regexp.MustCompile(`[ \t]+`)
)

// HTMLToText makes a readable plain-text version of an HTML body.
func HTMLToText(s string) string {
	s = reDrop.ReplaceAllString(s, "")
	s = reLi.ReplaceAllString(s, "- ")
	s = reBlockEnd.ReplaceAllString(s, "\n")
	s = reCell.ReplaceAllString(s, " ")
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, " ", " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(reSpaces.ReplaceAllString(l, " "))
	}
	s = strings.Join(lines, "\n")
	s = reBlank.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s) + "\n"
}

// Recipients returns every envelope recipient of m, for SMTP.
func Recipients(m *Outgoing) []string {
	var out []string
	for _, l := range [][]Address{m.To, m.Cc, m.Bcc} {
		for _, a := range l {
			out = append(out, a.Email)
		}
	}
	return out
}
