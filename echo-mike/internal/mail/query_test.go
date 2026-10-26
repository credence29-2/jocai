package mail

import (
	"testing"
	"time"
)

func TestParseQuery(t *testing.T) {
	q := ParseQuery(`printer from:derek subject:"october invoice" has:attachment after:2026-10-01 before:2026/10/20 to:billing`)
	if q.Text != "printer" || q.From != "derek" || q.Subject != "october invoice" || !q.HasAttachment || q.To != "billing" {
		t.Fatalf("parsed %+v", q)
	}
	if q.After.Format("2006-01-02") != "2026-10-01" || q.Before.Format("2006-01-02") != "2026-10-20" {
		t.Fatalf("dates %v %v", q.After, q.Before)
	}
	if ParseQuery("  ").IsEmpty() != true || ParseQuery("x").IsEmpty() {
		t.Fatal("IsEmpty")
	}
	if ParseQuery("has:thing time:now").Text != "has:thing time:now" {
		t.Fatal("unknown operators should stay text")
	}
	if s := q.String(); s != `printer from:derek to:billing subject:"october invoice" has:attachment after:2026-10-01 before:2026-10-20` {
		t.Fatalf("String = %q", s)
	}
}

func TestQueryMatches(t *testing.T) {
	m := &Message{Summary: Summary{
		From: Address{Name: "Derek Chan", Email: "derek@posdealer.example"}, To: []Address{{Email: "billing@credence.example"}},
		Subject: "Invoice question for October", Date: time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local), HasAttachments: true,
	}, HTML: "<p>our portal says 388</p>"}
	for _, c := range []struct {
		q    string
		want bool
	}{
		{"portal", true}, {"from:derek", true}, {"from:maria", false}, {"to:billing has:attachment", true},
		{"subject:invoice after:2026-10-01 before:2026-10-11", true}, {"before:2026-10-10", false}, {"after:2026-10-11", false},
		{"388 from:chan", true}, {"nothing here", false},
	} {
		if ParseQuery(c.q).Matches(m) != c.want {
			t.Errorf("%q: want %v", c.q, c.want)
		}
	}
}
