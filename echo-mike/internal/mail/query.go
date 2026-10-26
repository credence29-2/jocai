package mail

import (
	"strings"
	"time"
)

// Query is a search broken into its parts. The search box accepts the same
// operators Gmail does (from:, to:, subject:, has:attachment, after:,
// before:), and each provider renders a Query in its own syntax.
type Query struct {
	Text          string
	From          string
	To            string
	Subject       string
	HasAttachment bool
	// After and Before are dates; zero means no bound. Before is exclusive
	// of the day, like Gmail's.
	After, Before time.Time
}

// ParseQuery splits a search string into a Query. Quoted values keep their
// spaces: subject:"kitchen printer".
func ParseQuery(s string) Query {
	var q Query
	var text []string
	for _, tok := range tokens(s) {
		key, val, ok := strings.Cut(tok, ":")
		if !ok || val == "" {
			text = append(text, tok)
			continue
		}
		val = strings.Trim(val, `"`)
		switch strings.ToLower(key) {
		case "from":
			q.From = val
		case "to":
			q.To = val
		case "subject":
			q.Subject = val
		case "has":
			if strings.EqualFold(val, "attachment") {
				q.HasAttachment = true
			} else {
				text = append(text, tok)
			}
		case "after", "newer":
			q.After = parseDay(val)
		case "before", "older":
			q.Before = parseDay(val)
		default:
			text = append(text, tok)
		}
	}
	q.Text = strings.Join(text, " ")
	return q
}

// IsEmpty reports whether the query asks for nothing.
func (q Query) IsEmpty() bool {
	return q.Text == "" && q.From == "" && q.To == "" && q.Subject == "" && !q.HasAttachment && q.After.IsZero() && q.Before.IsZero()
}

// String renders the query back in the search box syntax.
func (q Query) String() string {
	var parts []string
	if q.Text != "" {
		parts = append(parts, q.Text)
	}
	add := func(k, v string) {
		if v == "" {
			return
		}
		if strings.ContainsAny(v, " \t") {
			v = `"` + v + `"`
		}
		parts = append(parts, k+":"+v)
	}
	add("from", q.From)
	add("to", q.To)
	add("subject", q.Subject)
	if q.HasAttachment {
		parts = append(parts, "has:attachment")
	}
	if !q.After.IsZero() {
		parts = append(parts, "after:"+q.After.Format("2006-01-02"))
	}
	if !q.Before.IsZero() {
		parts = append(parts, "before:"+q.Before.Format("2006-01-02"))
	}
	return strings.Join(parts, " ")
}

// Matches checks a message against the query, for providers that filter
// themselves (the sample accounts).
func (q Query) Matches(m *Message) bool {
	has := func(hay, needle string) bool {
		return needle == "" || strings.Contains(strings.ToLower(hay), strings.ToLower(needle))
	}
	addr := func(l []Address) string {
		var b strings.Builder
		for _, a := range l {
			b.WriteString(a.Name + " " + a.Email + " ")
		}
		return b.String()
	}
	if !has(m.From.Name+" "+m.From.Email, q.From) || !has(addr(m.To)+addr(m.Cc), q.To) || !has(m.Subject, q.Subject) {
		return false
	}
	if q.HasAttachment && !m.HasAttachments {
		return false
	}
	if !q.After.IsZero() && m.Date.Before(q.After) {
		return false
	}
	if !q.Before.IsZero() && !m.Date.Before(q.Before) {
		return false
	}
	return has(m.Subject+" "+m.From.Name+" "+m.From.Email+" "+m.Snippet+" "+HTMLToText(m.HTML)+" "+m.Text, q.Text)
}

func parseDay(s string) time.Time {
	for _, layout := range []string{"2006-01-02", "2006/01/02", "1/2/2006", "01/02/2006"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// tokens splits on spaces but keeps quoted strings together.
func tokens(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ' ' && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}
