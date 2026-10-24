package mail

import (
	"fmt"
	"html"
	"strings"
)

// Identity is an address the person can send as: an account's own address or
// one of its aliases, with the name and signature that go with it.
type Identity struct {
	ID            string `json:"id"`
	AccountID     string `json:"accountId"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	ReplyTo       string `json:"replyTo,omitempty"`
	SignatureHTML string `json:"signatureHtml"`
	// SignatureOnReplies adds the signature to replies and forwards too.
	SignatureOnReplies bool `json:"signatureOnReplies"`
	// Default is the account's preferred From address.
	Default bool `json:"default"`
	// Imported means it came from the provider (a Gmail send-as address)
	// rather than being typed in.
	Imported bool `json:"imported"`
}

// normEmail lowercases an address and drops a +tag so jacob+news@x.com
// matches jacob@x.com.
func normEmail(e string) string {
	e = strings.ToLower(strings.TrimSpace(e))
	at := strings.LastIndexByte(e, '@')
	if at < 0 {
		return e
	}
	local := e[:at]
	if p := strings.IndexByte(local, '+'); p > 0 {
		local = local[:p]
	}
	return local + e[at:]
}

// SameEmail reports whether two addresses are the same mailbox.
func SameEmail(a, b string) bool { return normEmail(a) == normEmail(b) }

// PickIdentity chooses the From address for a reply or forward of msg, which
// arrived in accountID. It prefers the alias the message was sent to, so a
// reply to mail sent to sales@ goes out from sales@ with that signature.
func PickIdentity(ids []Identity, accountID string, msg *Message) (Identity, bool) {
	var got []string
	if msg != nil {
		for _, l := range [][]Address{msg.To, msg.Cc, msg.DeliveredTo} {
			for _, a := range l {
				got = append(got, normEmail(a.Email))
			}
		}
	}
	// Addresses in To win over Cc, and Cc over Delivered-To. Within each,
	// an identity on the account the message arrived in wins.
	for _, g := range got {
		for _, sameAcct := range []bool{true, false} {
			for _, id := range ids {
				if (id.AccountID == accountID) == sameAcct && normEmail(id.Email) == g {
					return id, true
				}
			}
		}
	}
	return AccountDefault(ids, accountID)
}

// AccountDefault returns the account's default identity, or its first one.
func AccountDefault(ids []Identity, accountID string) (Identity, bool) {
	var first *Identity
	for i, id := range ids {
		if id.AccountID != accountID {
			continue
		}
		if id.Default {
			return id, true
		}
		if first == nil {
			first = &ids[i]
		}
	}
	if first != nil {
		return *first, true
	}
	return Identity{}, false
}

// ReplyKind is reply, reply all or forward.
type ReplyKind string

const (
	Reply    ReplyKind = "reply"
	ReplyAll ReplyKind = "replyAll"
	Forward  ReplyKind = "forward"
)

// Draft is a prefilled compose window.
type Draft struct {
	IdentityID string    `json:"identityId"`
	To         []Address `json:"to"`
	Cc         []Address `json:"cc"`
	Subject    string    `json:"subject"`
	// QuoteHTML goes under the signature.
	QuoteHTML  string `json:"quoteHtml"`
	InReplyTo  string `json:"inReplyTo,omitempty"`
	References string `json:"references,omitempty"`
	ThreadID   string `json:"threadId,omitempty"`
	// Attachments are the original's files, carried on a forward.
	Attachments []Attachment `json:"attachments,omitempty"`
}

// MakeDraft prefills a reply, reply all or forward of msg.
func MakeDraft(kind ReplyKind, ids []Identity, accountID string, msg *Message) Draft {
	from, _ := PickIdentity(ids, accountID, msg)
	d := Draft{IdentityID: from.ID, ThreadID: msg.ThreadID}
	mine := func(e string) bool {
		for _, id := range ids {
			if SameEmail(id.Email, e) {
				return true
			}
		}
		return false
	}

	if kind == Forward {
		d.Subject = prefixed("Fwd: ", msg.Subject)
		d.QuoteHTML = forwardHeader(msg) + bodyHTML(msg)
		for _, a := range msg.Attachments {
			if !a.Inline {
				d.Attachments = append(d.Attachments, a)
			}
		}
		return d
	}

	d.Subject = prefixed("Re: ", msg.Subject)
	d.InReplyTo = msg.MessageID
	d.References = strings.TrimSpace(msg.References + " " + msg.MessageID)

	sentByMe := mine(msg.From.Email)
	switch {
	case sentByMe:
		// Replying to your own sent message goes back to its recipients.
		d.To = append(d.To, msg.To...)
	case len(msg.ReplyTo) > 0:
		d.To = append(d.To, msg.ReplyTo...)
	default:
		d.To = []Address{msg.From}
	}
	if kind == ReplyAll {
		seen := map[string]bool{}
		for _, a := range d.To {
			seen[normEmail(a.Email)] = true
		}
		extra := msg.Cc
		if !sentByMe {
			extra = append(append([]Address{}, msg.To...), msg.Cc...)
		}
		for _, a := range extra {
			n := normEmail(a.Email)
			if seen[n] || mine(a.Email) {
				continue
			}
			seen[n] = true
			d.Cc = append(d.Cc, a)
		}
	}
	d.QuoteHTML = fmt.Sprintf(`<div class="cm-quote"><div>On %s, %s wrote:</div><blockquote style="margin:0 0 0 .8ex;border-left:1px solid #ccc;padding-left:1ex">%s</blockquote></div>`,
		html.EscapeString(msg.Date.Local().Format("Mon, Jan 2, 2006 at 3:04 PM")),
		html.EscapeString(displayAddr(msg.From)), bodyHTML(msg))
	return d
}

func forwardHeader(msg *Message) string {
	var b strings.Builder
	b.WriteString(`<div class="cm-quote"><div>---------- Forwarded message ---------</div>`)
	line := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "<div>%s: %s</div>", k, html.EscapeString(v))
		}
	}
	line("From", displayAddr(msg.From))
	line("Date", msg.Date.Local().Format("Mon, Jan 2, 2006 at 3:04 PM"))
	line("Subject", msg.Subject)
	line("To", joinAddrs(msg.To))
	line("Cc", joinAddrs(msg.Cc))
	b.WriteString("<br></div>")
	return b.String()
}

func bodyHTML(msg *Message) string {
	if msg.HTML != "" {
		return msg.HTML
	}
	return "<div style=\"white-space:pre-wrap\">" + html.EscapeString(msg.Text) + "</div>"
}

func displayAddr(a Address) string {
	if a.Name == "" {
		return a.Email
	}
	return a.Name + " <" + a.Email + ">"
}

func joinAddrs(l []Address) string {
	parts := make([]string, len(l))
	for i, a := range l {
		parts[i] = displayAddr(a)
	}
	return strings.Join(parts, ", ")
}

func prefixed(p, subject string) string {
	s := strings.TrimSpace(subject)
	low := strings.ToLower(s)
	already := []string{"re:"}
	if p == "Fwd: " {
		already = []string{"fwd:", "fw:"}
	}
	for _, a := range already {
		if strings.HasPrefix(low, a) {
			return s
		}
	}
	return p + s
}
