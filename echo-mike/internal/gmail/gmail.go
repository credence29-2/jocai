// Package gmail talks to the Gmail API.
package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"net/textproto"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	cm "github.com/credence29-2/jocai/echo-mike/internal/mail"
	"github.com/credence29-2/jocai/echo-mike/internal/oauth"
)

// Client is one Gmail account.
type Client struct {
	Src *oauth.Source
	// Base is the API root. Tests point it at a fake server.
	Base string

	// meta remembers message summaries for a while, so a refresh or the
	// new-mail check only fetches messages it hasn't seen. Gmail charges
	// each fetch against a per-user rate limit that a unified inbox with
	// several accounts can trip.
	metaMu sync.Mutex
	meta   map[string]cachedMeta
	// email is the account's address, learned on first use, to find its
	// own line in invitations.
	email string
}

type cachedMeta struct {
	sum cm.Summary
	at  time.Time
}

// metaTTL is how long a cached summary is trusted. Read and starred flags
// changed in another mail app can lag by this much; changes made here are
// applied to the cache right away.
const metaTTL = 5 * time.Minute

func (c *Client) cached(id string) (cm.Summary, bool) {
	c.metaMu.Lock()
	defer c.metaMu.Unlock()
	e, ok := c.meta[id]
	if !ok || time.Since(e.at) > metaTTL {
		return cm.Summary{}, false
	}
	return e.sum, true
}

func (c *Client) remember(s cm.Summary) {
	c.metaMu.Lock()
	defer c.metaMu.Unlock()
	if c.meta == nil || len(c.meta) > 4000 {
		c.meta = map[string]cachedMeta{}
	}
	c.meta[s.ID] = cachedMeta{sum: s, at: time.Now()}
}

// touch updates a cached summary's flags after a change made here.
func (c *Client) touch(id string, fn func(*cm.Summary)) {
	c.metaMu.Lock()
	defer c.metaMu.Unlock()
	if e, ok := c.meta[id]; ok {
		fn(&e.sum)
		c.meta[id] = e
	}
}

// New returns a client for the real Gmail API.
func New(src *oauth.Source) *Client {
	return &Client{Src: src, Base: "https://gmail.googleapis.com"}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	return c.doURL(ctx, method, c.Base+path, body, out)
}

func (c *Client) doURL(ctx context.Context, method, full string, body any, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	var req *http.Request
	var err error
	if rd != nil {
		req, err = http.NewRequestWithContext(ctx, method, full, rd)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, method, full, nil)
	}
	if err != nil {
		return err
	}
	return c.Src.Do(req, nil, out)
}

// Email returns the account's address.
func (c *Client) Email(ctx context.Context) (string, error) {
	var p struct {
		EmailAddress string `json:"emailAddress"`
	}
	err := c.do(ctx, "GET", "/gmail/v1/users/me/profile", nil, &p)
	if err == nil {
		c.email = p.EmailAddress
	}
	return p.EmailAddress, err
}

// folderQuery maps a unified folder to Gmail's label and search filters.
func folderQuery(f cm.Folder) (label, q string, spamTrash bool) {
	switch f {
	case cm.Inbox:
		return "INBOX", "", false
	case cm.Starred:
		return "STARRED", "", false
	case cm.Sent:
		return "SENT", "", false
	case cm.Spam:
		return "SPAM", "", true
	case cm.Trash:
		return "TRASH", "", true
	case cm.Drafts:
		return "DRAFT", "", false
	case cm.Archive:
		// Gmail has no Archive label; archived mail is everything that
		// isn't in the inbox and isn't only a sent copy or a draft.
		return "", "-in:inbox -in:sent -in:drafts -in:chats", false
	}
	return "", "", false
}

func (c *Client) List(ctx context.Context, f cm.Folder, query, cursor string, max int) (cm.Page, error) {
	v := url.Values{"maxResults": {strconv.Itoa(max)}}
	label, q, spamTrash := folderQuery(f)
	if query != "" {
		label, q, spamTrash = "", gmailQuery(query), false
	}
	if label != "" {
		v.Set("labelIds", label)
	}
	if q != "" {
		v.Set("q", q)
	}
	if spamTrash {
		v.Set("includeSpamTrash", "true")
	}
	if cursor != "" {
		v.Set("pageToken", cursor)
	}
	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := c.do(ctx, "GET", "/gmail/v1/users/me/messages?"+v.Encode(), nil, &list); err != nil {
		return cm.Page{}, err
	}

	// The list only has IDs, so fetch each message's headers, a few at a
	// time to stay under Gmail's rate limits.
	out := make([]cm.Summary, len(list.Messages))
	errs := make([]error, len(list.Messages))
	// A few at a time: Gmail allows about 250 units a second per user and
	// each fetch costs 5, so a burst of 30 from two accounts trips it.
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	meta := "?format=metadata&metadataHeaders=From&metadataHeaders=To&metadataHeaders=Subject"
	for i, m := range list.Messages {
		if s, ok := c.cached(m.ID); ok {
			out[i] = s
			continue
		}
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var gm message
			if errs[i] = c.do(ctx, "GET", "/gmail/v1/users/me/messages/"+url.PathEscape(id)+meta, nil, &gm); errs[i] == nil {
				out[i] = gm.summary()
				c.remember(out[i])
			}
		}(i, m.ID)
	}
	wg.Wait()
	var kept []cm.Summary
	for i, s := range out {
		if errs[i] != nil {
			if apiErr, ok := errs[i].(*oauth.APIError); ok && apiErr.Status == 404 {
				continue // deleted between the list and the fetch
			}
			return cm.Page{}, errs[i]
		}
		kept = append(kept, s)
	}
	if f == cm.Drafts && query == "" {
		if err := c.draftIDs(ctx, kept); err != nil {
			return cm.Page{}, err
		}
	}
	return cm.Page{Messages: kept, Next: list.NextPageToken}, nil
}

// gmailQuery renders a search in Gmail's syntax, which is the same as the
// app's except that dates use slashes.
func gmailQuery(query string) string {
	q := cm.ParseQuery(query)
	var parts []string
	if q.Text != "" {
		parts = append(parts, q.Text)
	}
	add := func(k, v string) {
		if v != "" {
			if strings.ContainsAny(v, " \t") {
				v = `"` + v + `"`
			}
			parts = append(parts, k+":"+v)
		}
	}
	add("from", q.From)
	add("to", q.To)
	add("subject", q.Subject)
	if q.HasAttachment {
		parts = append(parts, "has:attachment")
	}
	if !q.After.IsZero() {
		parts = append(parts, "after:"+q.After.Format("2006/01/02"))
	}
	if !q.Before.IsZero() {
		parts = append(parts, "before:"+q.Before.Format("2006/01/02"))
	}
	return strings.Join(parts, " ")
}

// draftIDs fills in each draft message's draft id, which is what the drafts
// API saves and deletes by.
func (c *Client) draftIDs(ctx context.Context, msgs []cm.Summary) error {
	var r struct {
		Drafts []struct {
			ID      string `json:"id"`
			Message struct {
				ID string `json:"id"`
			} `json:"message"`
		} `json:"drafts"`
	}
	if err := c.do(ctx, "GET", "/gmail/v1/users/me/drafts?maxResults=200", nil, &r); err != nil {
		return err
	}
	byMsg := map[string]string{}
	for _, d := range r.Drafts {
		byMsg[d.Message.ID] = d.ID
	}
	for i := range msgs {
		msgs[i].DraftID = byMsg[msgs[i].ID]
	}
	return nil
}

// SaveDraft creates or replaces a draft. Gmail gives the replaced draft a new
// message id, so the list is re-read after saving.
func (c *Client) SaveDraft(ctx context.Context, draftID string, o *cm.Outgoing) (string, error) {
	raw, err := cm.Build(o, true, time.Now())
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	meta, _ := json.Marshal(map[string]any{"message": map[string]string{"threadId": o.ThreadID}})
	w, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	w.Write(meta)
	w, _ = mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"message/rfc822"}})
	w.Write(raw)
	mw.Close()
	method, path := "POST", "/upload/gmail/v1/users/me/drafts?uploadType=multipart"
	if draftID != "" {
		method, path = "PUT", "/upload/gmail/v1/users/me/drafts/"+url.PathEscape(draftID)+"?uploadType=multipart"
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, bytes.NewReader(body.Bytes()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "multipart/related; boundary="+mw.Boundary())
	var out struct {
		ID string `json:"id"`
	}
	if err := c.Src.Do(req, nil, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// Respond answers an invitation the way mail programs without calendar
// access do: a calendar reply emailed to the organizer. Google Calendar
// reads it and updates the attendee list, and adds the event to this
// account's calendar on its own.
func (c *Client) Respond(ctx context.Context, msgID string, inv *cm.Invite, me cm.Address, status string) error {
	status = strings.ToUpper(status)
	o := &cm.Outgoing{
		From:      me,
		To:        []cm.Address{inv.Organizer},
		Subject:   cm.ReplyWords(inv, status),
		HTML:      "<p>" + cm.ReplyWords(inv, status) + "</p>",
		ICS:       cm.ReplyICS(inv, me, status, time.Now()),
		ICSMethod: "REPLY",
	}
	return c.Send(ctx, o)
}

func (c *Client) DeleteDraft(ctx context.Context, draftID string) error {
	err := c.do(ctx, "DELETE", "/gmail/v1/users/me/drafts/"+url.PathEscape(draftID), nil, nil)
	if apiErr, ok := err.(*oauth.APIError); ok && apiErr.Status == 404 {
		return nil
	}
	return err
}

type part struct {
	PartID   string `json:"partId"`
	MimeType string `json:"mimeType"`
	Filename string `json:"filename"`
	Headers  []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"headers"`
	Body struct {
		AttachmentID string `json:"attachmentId"`
		Size         int64  `json:"size"`
		Data         string `json:"data"`
	} `json:"body"`
	Parts []part `json:"parts"`
}

func (p *part) header(name string) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func (p *part) headers(name string) []string {
	var out []string
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			out = append(out, h.Value)
		}
	}
	return out
}

type message struct {
	ID           string   `json:"id"`
	ThreadID     string   `json:"threadId"`
	LabelIDs     []string `json:"labelIds"`
	Snippet      string   `json:"snippet"`
	InternalDate string   `json:"internalDate"`
	Payload      part     `json:"payload"`
}

func (m *message) has(label string) bool {
	for _, l := range m.LabelIDs {
		if l == label {
			return true
		}
	}
	return false
}

func (m *message) summary() cm.Summary {
	ms, _ := strconv.ParseInt(m.InternalDate, 10, 64)
	from := ParseAddrs(m.Payload.header("From"))
	s := cm.Summary{
		ID: m.ID, ThreadID: m.ThreadID,
		To:      ParseAddrs(m.Payload.header("To")),
		Subject: decodeHeader(m.Payload.header("Subject")),
		Snippet: htmlUnescape(m.Snippet),
		Date:    time.UnixMilli(ms),
		Unread:  m.has("UNREAD"), Starred: m.has("STARRED"),
		HasAttachments: strings.EqualFold(m.Payload.MimeType, "multipart/mixed"),
	}
	if len(from) > 0 {
		s.From = from[0]
	}
	return s
}

func (c *Client) Get(ctx context.Context, id string) (*cm.Message, error) {
	var gm message
	if err := c.do(ctx, "GET", "/gmail/v1/users/me/messages/"+url.PathEscape(id)+"?format=full", nil, &gm); err != nil {
		return nil, err
	}
	return c.full(ctx, &gm)
}

// Thread returns a conversation's messages, oldest first.
func (c *Client) Thread(ctx context.Context, threadID string) ([]*cm.Message, error) {
	var t struct {
		Messages []message `json:"messages"`
	}
	if err := c.do(ctx, "GET", "/gmail/v1/users/me/threads/"+url.PathEscape(threadID)+"?format=full", nil, &t); err != nil {
		return nil, err
	}
	out := make([]*cm.Message, 0, len(t.Messages))
	for i := range t.Messages {
		m, err := c.full(ctx, &t.Messages[i])
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out, nil
}

// full turns a format=full message into a Message with body and attachments.
func (c *Client) full(ctx context.Context, gm *message) (*cm.Message, error) {
	id := gm.ID
	p := &gm.Payload
	m := &cm.Message{
		Summary:    gm.summary(),
		Cc:         ParseAddrs(p.header("Cc")),
		Bcc:        ParseAddrs(p.header("Bcc")),
		ReplyTo:    ParseAddrs(p.header("Reply-To")),
		InReplyTo:  p.header("In-Reply-To"),
		MessageID:  p.header("Message-ID"),
		References: p.header("References"),
	}
	m.HasAttachments = false
	m.Hidden = gm.has("TRASH") || gm.has("SPAM")
	defer func() { c.remember(m.Summary) }()
	for _, h := range append(p.headers("Delivered-To"), p.headers("X-Original-To")...) {
		m.DeliveredTo = append(m.DeliveredTo, ParseAddrs(h)...)
	}
	if err := c.walk(ctx, id, p, m); err != nil {
		return nil, err
	}
	for _, a := range m.Attachments {
		if !a.Inline {
			m.HasAttachments = true
		}
	}
	return m, nil
}

// walk collects the body and attachments from the MIME tree. For
// multipart/alternative it keeps the last (richest) version Gmail gives.
func (c *Client) walk(ctx context.Context, msgID string, p *part, m *cm.Message) error {
	typ, params, _ := mime.ParseMediaType(p.MimeType)
	if typ == "" {
		typ = strings.ToLower(p.MimeType)
	}
	if ct := p.header("Content-Type"); ct != "" {
		if _, pr, err := mime.ParseMediaType(ct); err == nil {
			params = pr
		}
	}
	disp, _, _ := mime.ParseMediaType(p.header("Content-Disposition"))
	cid := strings.Trim(p.header("Content-ID"), "<> ")

	if strings.HasPrefix(typ, "multipart/") {
		for i := range p.Parts {
			if err := c.walk(ctx, msgID, &p.Parts[i], m); err != nil {
				return err
			}
		}
		return nil
	}
	if typ == "text/calendar" && m.Invite == nil {
		// An invitation. The part is also kept as an attachment below when
		// it's a file, so it can be saved.
		if data, err := c.partData(ctx, msgID, p); err == nil {
			if inv, err := cm.ParseICS(cm.DecodeCharset(data, params["charset"]), c.email); err == nil && inv.Method != "REPLY" {
				m.Invite = inv
			}
		}
		if p.Filename == "" {
			return nil // the bare calendar part isn't a file to save
		}
	}
	isFile := p.Filename != "" || disp == "attachment" || (cid != "" && !strings.HasPrefix(typ, "text/"))
	if !isFile && (typ == "text/html" || typ == "text/plain") {
		data, err := c.partData(ctx, msgID, p)
		if err != nil {
			return err
		}
		text := cm.DecodeCharset(data, params["charset"])
		if typ == "text/html" {
			m.HTML += text
		} else if m.Text == "" {
			m.Text = text
		} else {
			m.Text += "\n" + text
		}
		return nil
	}
	if p.Body.AttachmentID == "" && p.Body.Data == "" {
		return nil
	}
	name := p.Filename
	if name == "" {
		name = params["name"]
	}
	if name == "" {
		name = "attachment"
	}
	id := p.Body.AttachmentID
	if id == "" {
		// Tiny parts come inline in the message; address them by part ID.
		id = "part:" + p.PartID
	}
	m.Attachments = append(m.Attachments, cm.Attachment{
		ID: id, Name: decodeHeader(name), MIMEType: typ, Size: p.Body.Size,
		ContentID: cid, Inline: cid != "" && disp != "attachment",
	})
	return nil
}

func (c *Client) partData(ctx context.Context, msgID string, p *part) ([]byte, error) {
	if p.Body.Data != "" || p.Body.AttachmentID == "" {
		return decodeB64(p.Body.Data)
	}
	return c.Attachment(ctx, msgID, p.Body.AttachmentID)
}

func (c *Client) Attachment(ctx context.Context, msgID, attID string) ([]byte, error) {
	if pid, ok := strings.CutPrefix(attID, "part:"); ok {
		var gm message
		if err := c.do(ctx, "GET", "/gmail/v1/users/me/messages/"+url.PathEscape(msgID)+"?format=full", nil, &gm); err != nil {
			return nil, err
		}
		if p := findPart(&gm.Payload, pid); p != nil {
			return decodeB64(p.Body.Data)
		}
		return nil, fmt.Errorf("attachment not found")
	}
	var a struct {
		Data string `json:"data"`
	}
	path := "/gmail/v1/users/me/messages/" + url.PathEscape(msgID) + "/attachments/" + url.PathEscape(attID)
	if err := c.do(ctx, "GET", path, nil, &a); err != nil {
		return nil, err
	}
	return decodeB64(a.Data)
}

func findPart(p *part, id string) *part {
	if p.PartID == id {
		return p
	}
	for i := range p.Parts {
		if f := findPart(&p.Parts[i], id); f != nil {
			return f
		}
	}
	return nil
}

func (c *Client) Apply(ctx context.Context, id string, ch cm.Change) error {
	defer c.touch(id, func(s *cm.Summary) {
		switch ch {
		case cm.MarkRead:
			s.Unread = false
		case cm.MarkUnread:
			s.Unread = true
		case cm.Star:
			s.Starred = true
		case cm.Unstar:
			s.Starred = false
		}
	})
	base := "/gmail/v1/users/me/messages/" + url.PathEscape(id)
	modify := func(add, remove []string) error {
		return c.do(ctx, "POST", base+"/modify", map[string][]string{"addLabelIds": add, "removeLabelIds": remove}, nil)
	}
	switch ch {
	case cm.MarkRead:
		return modify(nil, []string{"UNREAD"})
	case cm.MarkUnread:
		return modify([]string{"UNREAD"}, nil)
	case cm.Star:
		return modify([]string{"STARRED"}, nil)
	case cm.Unstar:
		return modify(nil, []string{"STARRED"})
	case cm.MoveArchive:
		return modify(nil, []string{"INBOX"})
	case cm.MoveSpam:
		return modify([]string{"SPAM"}, []string{"INBOX"})
	case cm.MoveTrash:
		return c.do(ctx, "POST", base+"/trash", nil, nil)
	case cm.MoveInbox:
		if err := c.do(ctx, "POST", base+"/untrash", nil, nil); err != nil {
			return err
		}
		return modify([]string{"INBOX"}, []string{"SPAM"})
	}
	return fmt.Errorf("unknown change %q", ch)
}

// Send uploads the message as raw MIME. Gmail sends it from the From address
// as long as that's a verified send-as alias on the account, and files it in
// the Sent label and the right conversation.
func (c *Client) Send(ctx context.Context, o *cm.Outgoing) error {
	raw, err := cm.Build(o, true, time.Now())
	if err != nil {
		return err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	meta, _ := json.Marshal(map[string]string{"threadId": o.ThreadID})
	w, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	w.Write(meta)
	w, _ = mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"message/rfc822"}})
	w.Write(raw)
	mw.Close()
	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+"/upload/gmail/v1/users/me/messages/send?uploadType=multipart", bytes.NewReader(body.Bytes()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "multipart/related; boundary="+mw.Boundary())
	return c.Src.Do(req, nil, nil)
}

func (c *Client) Aliases(ctx context.Context) ([]cm.Alias, error) {
	var r struct {
		SendAs []struct {
			SendAsEmail        string `json:"sendAsEmail"`
			DisplayName        string `json:"displayName"`
			ReplyToAddress     string `json:"replyToAddress"`
			Signature          string `json:"signature"`
			IsPrimary          bool   `json:"isPrimary"`
			IsDefault          bool   `json:"isDefault"`
			VerificationStatus string `json:"verificationStatus"`
		} `json:"sendAs"`
	}
	if err := c.do(ctx, "GET", "/gmail/v1/users/me/settings/sendAs", nil, &r); err != nil {
		return nil, err
	}
	var out []cm.Alias
	for _, s := range r.SendAs {
		// Unverified aliases can't send yet; Gmail would swap in the main
		// address.
		if !s.IsPrimary && s.VerificationStatus != "accepted" {
			continue
		}
		out = append(out, cm.Alias{
			Email: s.SendAsEmail, Name: s.DisplayName, ReplyTo: s.ReplyToAddress,
			SignatureHTML: s.Signature, Primary: s.IsPrimary, Default: s.IsDefault,
		})
	}
	return out, nil
}

func (c *Client) Unread(ctx context.Context) (int, error) {
	var l struct {
		MessagesUnread int `json:"messagesUnread"`
	}
	err := c.do(ctx, "GET", "/gmail/v1/users/me/labels/INBOX", nil, &l)
	return l.MessagesUnread, err
}

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		// Some clients send standard base64; accept that too.
		return base64.RawStdEncoding.DecodeString(s)
	}
	return b, nil
}

var wordDec = &mime.WordDecoder{CharsetReader: cm.CharsetReader}

func decodeHeader(s string) string {
	if d, err := wordDec.DecodeHeader(s); err == nil {
		return d
	}
	return s
}

// ParseAddrs reads an address header, falling back to a bare value when the
// header isn't strictly valid, which happens a lot in real mail.
func ParseAddrs(h string) []cm.Address {
	h = strings.TrimSpace(h)
	if h == "" {
		return nil
	}
	p := mail.AddressParser{WordDecoder: wordDec}
	if list, err := p.ParseList(h); err == nil {
		out := make([]cm.Address, len(list))
		for i, a := range list {
			out[i] = cm.Address{Name: a.Name, Email: a.Address}
		}
		return out
	}
	var out []cm.Address
	for _, piece := range strings.Split(h, ",") {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			continue
		}
		if a, err := p.Parse(piece); err == nil {
			out = append(out, cm.Address{Name: a.Name, Email: a.Address})
			continue
		}
		email := piece
		if i, j := strings.LastIndex(piece, "<"), strings.LastIndex(piece, ">"); i >= 0 && j > i {
			email = piece[i+1 : j]
		}
		out = append(out, cm.Address{Email: strings.TrimSpace(email), Name: decodeHeader(strings.Trim(piece[:max(0, strings.LastIndex(piece, "<"))], ` "`))})
	}
	return out
}

var unescaper = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")

func htmlUnescape(s string) string { return unescaper.Replace(s) }
