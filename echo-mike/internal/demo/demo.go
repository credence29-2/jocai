// Package demo is a fake mailbox with sample messages, for trying the app
// and for tests, without signing in to anything.
package demo

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

type msg struct {
	m      mail.Message
	folder mail.Folder
	files  map[string][]byte
}

// Box is one fake account.
type Box struct {
	Email   string
	aliases []mail.Alias

	mu   sync.Mutex
	msgs []*msg
	next int
	// SentLog records everything Send was given, for tests.
	SentLog []mail.Outgoing

	events []mail.Event
	nextEv int
}

type seed struct {
	from    mail.Address
	to      string
	subject string
	body    string
	ago     time.Duration
	unread  bool
	starred bool
	folder  mail.Folder
	file    string
	// thread puts the message in an earlier message's conversation.
	thread string
	labels []string
}

// Boxes returns two sample accounts: a Gmail-style one with aliases and an
// Outlook-style one.
func Boxes(now time.Time) (*Box, *Box) {
	g := &Box{Email: "jacob.demo@gmail.example", aliases: []mail.Alias{
		{Email: "jacob.demo@gmail.example", Name: "Jacob", Primary: true, Default: true, SignatureHTML: "Jacob<br>Sent from my personal account"},
		{Email: "support@credence.example", Name: "Credence Support", ReplyTo: "help@credence.example", SignatureHTML: "<b>Jacob</b><br>General Manager, Credence Support<br>(555) 010-2200"},
		{Email: "billing@credence.example", Name: "Credence Billing", SignatureHTML: "Credence Billing Team<br>billing@credence.example"},
	}}
	o := &Box{Email: "jacob.demo@outlook.example", aliases: []mail.Alias{
		{Email: "jacob.demo@outlook.example", Name: "Jacob", Primary: true, Default: true},
	}}
	h := time.Hour
	g.seed(now, []seed{
		{mail.Address{Name: "Maria Lopez", Email: "maria@tacoshack.example"}, "support@credence.example", "Terminal 3 won't print kitchen tickets", "<p>Hi team,</p><p>Since this morning terminal 3 stopped sending tickets to the kitchen printer. The other two terminals are fine. We already rebooted it twice.</p><p>Can someone call me before the lunch rush? 555-0134.</p><p>Thanks,<br>Maria</p>", 25 * time.Minute, true, true, mail.Inbox, "", "", []string{"Label_1"}},
		{mail.Address{Name: "Credence Support", Email: "support@credence.example"}, "maria@tacoshack.example", "Re: Terminal 3 won't print kitchen tickets", "<p>Hi Maria, is the printer's light solid green or blinking? If it's blinking, hold the feed button for 5 seconds and let me know what prints.</p><p>Jacob</p>", 18 * time.Minute, false, false, mail.Sent, "", "1", nil},
		{mail.Address{Name: "Maria Lopez", Email: "maria@tacoshack.example"}, "support@credence.example", "Re: Terminal 3 won't print kitchen tickets", "<p>Blinking. I held the button and it printed a test page with an IP of 0.0.0.0.</p><p>Maria</p>", 9 * time.Minute, true, false, mail.Inbox, "", "1", nil},
		{mail.Address{Name: "Zendesk", Email: "notifications@zendesk.example"}, "jacob.demo@gmail.example", "[Credence] 14 tickets breached first reply SLA yesterday", "<p>Your daily SLA summary is ready.</p><table border=1 cellpadding=4><tr><th>Group</th><th>Breached</th></tr><tr><td>Tier 1</td><td>11</td></tr><tr><td>Escalations</td><td>3</td></tr></table><p><a href=\"https://example.com/sla\">Open the report</a></p><img src=\"https://example.com/pixel.gif\" width=1 height=1>", 2 * h, true, false, mail.Inbox, "", "", nil},
		{mail.Address{Name: "Derek Chan", Email: "derek@posdealer.example"}, "billing@credence.example", "Invoice question for October", "<p>Hey, the October invoice shows 412 tickets but our portal says 388. Can you send the breakdown?</p><p>Derek</p>", 5 * h, false, false, mail.Inbox, "october-ticket-export.csv", "", []string{"Label_1", "Label_2"}},
		{mail.Address{Name: "Mom", Email: "mom@family.example"}, "jacob.demo@gmail.example", "Sunday dinner", "<p>Are you still bringing the dessert? Let me know by Friday.</p>", 27 * h, false, false, mail.Inbox, "", "", nil},
		{mail.Address{Name: "Credence Support", Email: "support@credence.example"}, "maria@tacoshack.example", "Re: Online orders not syncing", "<p>Hi Maria, the sync is fixed. Orders placed between 2 and 3 PM were re-sent to the POS.</p>", 50 * h, false, false, mail.Sent, "", "", nil},
		{mail.Address{Name: "Airline", Email: "deals@airline.example"}, "jacob.demo@gmail.example", "Fares from $49 this weekend only", "<p>Book now.</p>", 70 * h, false, false, mail.Spam, "", "", nil},
		{mail.Address{Name: "Ana Ruiz", Email: "ana@credence.example"}, "support@credence.example", "Weekend schedule swap", "<p>Can I swap Saturday with Kevin's Sunday shift? He already said yes.</p>", 96 * h, false, false, mail.Archive, "", "", []string{"Label_3"}},
	})
	o.seed(now, []seed{
		{mail.Address{Name: "Priya Nair", Email: "priya@restauranttech.example"}, "jacob.demo@outlook.example", "Onboarding call for the new Toast dealer", "<p>Jacob,</p><p>Can we do Thursday at 2 for the onboarding kickoff? I'll bring the escalation matrix draft.</p><p>Priya</p>", 50 * time.Minute, true, false, mail.Inbox, "", "", nil},
		{mail.Address{Name: "Microsoft 365", Email: "no-reply@microsoft.example"}, "jacob.demo@outlook.example", "Your storage is 80% full", "<p>Free up space or upgrade.</p>", 9 * h, false, false, mail.Inbox, "", "", nil},
		{mail.Address{Name: "Kevin Brooks", Email: "kevin@credence.example"}, "jacob.demo@outlook.example", "Missed calls report draft", "<p>Attached is the draft. Callback rate is up to 91% from 84% last month.</p>", 30 * h, false, true, mail.Inbox, "missed-calls-draft.pdf", "", nil},
	})
	// Priya's message carries an invitation.
	for _, m := range o.msgs {
		if m.m.From.Email == "priya@restauranttech.example" {
			ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nMETHOD:REQUEST\r\nBEGIN:VEVENT\r\nUID:demo-kickoff@restauranttech.example\r\nSEQUENCE:0\r\n" +
				"ORGANIZER;CN=Priya Nair:mailto:priya@restauranttech.example\r\n" +
				"ATTENDEE;CN=Jacob;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:" + o.Email + "\r\n" +
				"ATTENDEE;CN=Kevin Brooks;PARTSTAT=ACCEPTED:mailto:kevin@credence.example\r\n" +
				"SUMMARY:Onboarding kickoff - Toast dealer\r\nLOCATION:Teams\r\nDESCRIPTION:Escalation matrix draft and support hours.\r\n" +
				"DTSTART:" + nextThursday(now).Format("20060102T150405Z") + "\r\nDTEND:" + nextThursday(now).Add(time.Hour).Format("20060102T150405Z") + "\r\n" +
				"END:VEVENT\r\nEND:VCALENDAR\r\n"
			m.m.Invite, _ = mail.ParseICS(ics, o.Email)
			m.files["f1"] = []byte(ics)
			m.m.Attachments = append(m.m.Attachments, mail.Attachment{ID: "f1", Name: "invite.ics", MIMEType: "text/calendar", Size: int64(len(ics))})
		}
	}
	g.seedEvents(now)
	o.seedEvents(now)
	return g, o
}

// nextThursday is 2 PM local on the coming Thursday, in UTC.
func nextThursday(now time.Time) time.Time {
	d := now
	for d.Weekday() != time.Thursday || !d.After(now) {
		d = d.Add(24 * time.Hour)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 14, 0, 0, 0, time.Local).UTC()
}

func (b *Box) seed(now time.Time, list []seed) {
	for _, s := range list {
		b.next++
		id := strconv.Itoa(b.next)
		m := &msg{folder: s.folder, files: map[string][]byte{}}
		thread := s.thread
		if thread == "" {
			thread = id
		}
		m.m = mail.Message{
			Summary: mail.Summary{
				ID: id, ThreadID: thread, From: s.from, To: []mail.Address{{Email: s.to}},
				Subject: s.subject, Snippet: snippet(s.body), Date: now.Add(-s.ago),
				Unread: s.unread, Starred: s.starred, HasAttachments: s.file != "", Labels: s.labels,
			},
			MessageID: fmt.Sprintf("<demo-%s@%s>", id, b.Email),
			HTML:      s.body,
		}
		if s.file != "" {
			m.files["f1"] = sampleFile(s.file)
			m.m.Attachments = []mail.Attachment{{ID: "f1", Name: s.file, MIMEType: mimeFor(s.file), Size: int64(len(m.files["f1"]))}}
		}
		b.msgs = append(b.msgs, m)
	}
}

// sampleFile returns something that opens for the sample attachments: a
// one-page PDF, a small PNG, or plain text.
func sampleFile(name string) []byte {
	switch {
	case strings.HasSuffix(name, ".pdf"):
		return []byte("%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
			"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj\n" +
			"4 0 obj<</Length 96>>stream\nBT /F1 24 Tf 72 700 Td (Missed calls report, draft) Tj 0 -40 Td /F1 14 Tf (Callback rate: 91%) Tj ET\nendstream\nendobj\n" +
			"5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n")
	case strings.HasSuffix(name, ".png"):
		b, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")
		return b
	}
	return []byte("Sample attachment " + name + "\n")
}

func mimeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(name, ".csv"):
		return "text/csv"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	}
	return "application/octet-stream"
}

func snippet(html string) string {
	t := strings.Join(strings.Fields(mail.HTMLToText(html)), " ")
	if len(t) > 140 {
		t = t[:140]
	}
	return t
}

func (b *Box) find(id string) (*msg, error) {
	for _, m := range b.msgs {
		if m.m.ID == id {
			return m, nil
		}
	}
	return nil, fmt.Errorf("message not found")
}

func (b *Box) List(_ context.Context, f mail.Folder, query, cursor string, max int) (mail.Page, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := mail.ParseQuery(query)
	var out []mail.Summary
	for _, m := range b.msgs {
		in := m.folder == f || f == mail.Starred && m.m.Starred && m.folder != mail.Trash
		if id, ok := mail.LabelID(f); ok {
			in = m.folder != mail.Trash && m.folder != mail.Spam
			if in {
				in = false
				for _, l := range m.m.Labels {
					if l == id {
						in = true
					}
				}
			}
		}
		if !q.IsEmpty() {
			in = m.folder != mail.Trash && m.folder != mail.Spam && q.Matches(&m.m)
		}
		if in {
			out = append(out, m.m.Summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	start, _ := strconv.Atoi(cursor)
	if start > len(out) {
		start = len(out)
	}
	end := start + max
	next := ""
	if end < len(out) {
		next = strconv.Itoa(end)
	} else {
		end = len(out)
	}
	return mail.Page{Messages: out[start:end], Next: next}, nil
}

func (b *Box) Thread(_ context.Context, threadID string) ([]*mail.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*mail.Message
	for _, m := range b.msgs {
		if m.m.ThreadID != threadID {
			continue
		}
		c := m.m
		c.Hidden = m.folder == mail.Trash || m.folder == mail.Spam
		out = append(out, &c)
	}
	if out == nil {
		return nil, fmt.Errorf("conversation not found")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out, nil
}

func (b *Box) Get(_ context.Context, id string) (*mail.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, err := b.find(id)
	if err != nil {
		return nil, err
	}
	c := m.m
	return &c, nil
}

func (b *Box) Apply(_ context.Context, id string, c mail.Change) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, err := b.find(id)
	if err != nil {
		return err
	}
	switch c {
	case mail.MarkRead:
		m.m.Unread = false
	case mail.MarkUnread:
		m.m.Unread = true
	case mail.Star:
		m.m.Starred = true
	case mail.Unstar:
		m.m.Starred = false
	case mail.MoveArchive:
		m.folder = mail.Archive
	case mail.MoveTrash:
		m.folder = mail.Trash
	case mail.MoveInbox:
		m.folder = mail.Inbox
	case mail.MoveSpam:
		m.folder = mail.Spam
	default:
		return fmt.Errorf("unknown change %q", c)
	}
	return nil
}

func (b *Box) Attachment(_ context.Context, msgID, attID string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, err := b.find(msgID)
	if err != nil {
		return nil, err
	}
	data, ok := m.files[attID]
	if !ok {
		return nil, fmt.Errorf("attachment not found")
	}
	return data, nil
}

func (b *Box) Send(_ context.Context, o *mail.Outgoing) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.SentLog = append(b.SentLog, *o)
	b.next++
	id := strconv.Itoa(b.next)
	m := &msg{folder: mail.Sent, files: map[string][]byte{}}
	m.m = mail.Message{
		Summary: mail.Summary{ID: id, ThreadID: o.ThreadID, From: o.From, To: o.To, Subject: o.Subject,
			Snippet: snippet(o.HTML), Date: time.Now(), HasAttachments: len(o.Files) > 0},
		Cc: o.Cc, HTML: o.HTML,
	}
	for i, f := range o.Files {
		aid := "f" + strconv.Itoa(i+1)
		m.files[aid] = f.Data
		m.m.Attachments = append(m.m.Attachments, mail.Attachment{ID: aid, Name: f.Name, MIMEType: f.MIMEType, Size: int64(len(f.Data))})
	}
	b.msgs = append(b.msgs, m)
	return nil
}

// SaveDraft keeps the draft as a message in the Drafts folder.
func (b *Box) SaveDraft(_ context.Context, draftID string, o *mail.Outgoing) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var m *msg
	if draftID != "" {
		for _, x := range b.msgs {
			if x.m.ID == draftID && x.folder == mail.Drafts {
				m = x
			}
		}
	}
	if m == nil {
		b.next++
		m = &msg{folder: mail.Drafts}
		m.m.ID = strconv.Itoa(b.next)
		b.msgs = append(b.msgs, m)
	}
	id := m.m.ID
	m.m = mail.Message{
		Summary: mail.Summary{ID: id, ThreadID: o.ThreadID, DraftID: id, From: o.From, To: o.To, Subject: o.Subject,
			Snippet: snippet(o.HTML), Date: time.Now(), HasAttachments: len(o.Files) > 0},
		Cc: o.Cc, Bcc: o.Bcc, HTML: o.HTML, InReplyTo: o.InReplyTo, References: o.References,
	}
	if m.m.ThreadID == "" {
		m.m.ThreadID = id
	}
	m.files = map[string][]byte{}
	for i, f := range o.Files {
		aid := "f" + strconv.Itoa(i+1)
		m.files[aid] = f.Data
		m.m.Attachments = append(m.m.Attachments, mail.Attachment{ID: aid, Name: f.Name, MIMEType: f.MIMEType, Size: int64(len(f.Data))})
	}
	return id, nil
}

func (b *Box) DeleteDraft(_ context.Context, draftID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, x := range b.msgs {
		if x.m.ID == draftID && x.folder == mail.Drafts {
			b.msgs = append(b.msgs[:i], b.msgs[i+1:]...)
			return nil
		}
	}
	return nil
}

// Respond records the answer and marks the attendee line.
func (b *Box) Respond(_ context.Context, msgID string, inv *mail.Invite, me mail.Address, status string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, err := b.find(msgID)
	if err != nil {
		return err
	}
	if m.m.Invite == nil {
		return fmt.Errorf("that message has no invitation")
	}
	m.m.Invite.MyStatus = strings.ToUpper(status)
	b.SentLog = append(b.SentLog, mail.Outgoing{From: me, To: []mail.Address{inv.Organizer}, Subject: mail.ReplyWords(inv, status), ICSMethod: "REPLY"})
	return nil
}

func (b *Box) Aliases(context.Context) ([]mail.Alias, error) {
	return append([]mail.Alias(nil), b.aliases...), nil
}

// Deliver puts a new unread message in the inbox, as if it just arrived.
func (b *Box) Deliver(from mail.Address, subject, body string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seed(time.Now(), []seed{{from, b.Email, subject, body, 0, true, false, mail.Inbox, "", "", nil}})
}

func (b *Box) Unread(context.Context) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, m := range b.msgs {
		if m.folder == mail.Inbox && m.m.Unread {
			n++
		}
	}
	return n, nil
}
