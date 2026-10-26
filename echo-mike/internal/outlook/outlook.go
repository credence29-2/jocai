// Package outlook reads Outlook.com and Microsoft 365 mail through Microsoft
// Graph and sends it over SMTP with the same sign-in.
//
// Sending goes through SMTP because Graph's sendMail always puts the primary
// address in From, even when the message names an alias. SMTP takes the From
// header as written, so aliases work.
package outlook

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	cm "github.com/credence29-2/jocai/echo-mike/internal/mail"
	"github.com/credence29-2/jocai/echo-mike/internal/oauth"
)

// Client is one Microsoft mailbox.
type Client struct {
	Src *oauth.Source
	// Email is the address the account signs in with.
	Email string
	// Base is the Graph root. Tests point it at a fake server.
	Base string
	SMTP SMTPConfig

	// hiddenFolders holds the IDs of Deleted Items and Junk Email, looked
	// up once, so thread messages in them can be marked Hidden.
	hiddenOnce    sync.Once
	hiddenFolders map[string]bool
}

// New returns a client for the real Graph and SMTP servers.
func New(src *oauth.Source, email string) *Client {
	return &Client{Src: src, Email: email, Base: "https://graph.microsoft.com/v1.0", SMTP: smtpFor(email)}
}

func (c *Client) do(ctx context.Context, method, rawURL string, body any, out any) error {
	var req *http.Request
	var err error
	if body != nil {
		b, merr := json.Marshal(body)
		if merr != nil {
			return merr
		}
		req, err = http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(b))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, method, rawURL, nil)
	}
	if err != nil {
		return err
	}
	// Immutable IDs stay the same when a message moves folders, so an Undo
	// after Archive still finds it.
	req.Header.Add("Prefer", `IdType="ImmutableId"`)
	req.Header.Add("Prefer", `outlook.body-content-type="html"`)
	return c.Src.Do(req, nil, out)
}

// Me returns the signed-in address and display name.
func (c *Client) Me(ctx context.Context) (email, name string, err error) {
	var me struct {
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
		DisplayName       string `json:"displayName"`
	}
	if err := c.do(ctx, "GET", c.Base+"/me?$select=mail,userPrincipalName,displayName", nil, &me); err != nil {
		return "", "", err
	}
	email = me.Mail
	if email == "" {
		email = me.UserPrincipalName
	}
	return email, me.DisplayName, nil
}

var wellKnown = map[cm.Folder]string{
	cm.Inbox: "inbox", cm.Sent: "sentitems", cm.Drafts: "drafts", cm.Archive: "archive", cm.Spam: "junkemail", cm.Trash: "deleteditems",
}

const listFields = "id,conversationId,from,toRecipients,subject,bodyPreview,receivedDateTime,sentDateTime,isRead,flag,hasAttachments,isDraft"

type gAddr struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

func (a gAddr) addr() cm.Address {
	return cm.Address{Name: a.EmailAddress.Name, Email: a.EmailAddress.Address}
}

func addrs(l []gAddr) []cm.Address {
	out := make([]cm.Address, 0, len(l))
	for _, a := range l {
		out = append(out, a.addr())
	}
	return out
}

type gMessage struct {
	ID               string    `json:"id"`
	ConversationID   string    `json:"conversationId"`
	From             *gAddr    `json:"from"`
	To               []gAddr   `json:"toRecipients"`
	Cc               []gAddr   `json:"ccRecipients"`
	Bcc              []gAddr   `json:"bccRecipients"`
	ReplyTo          []gAddr   `json:"replyTo"`
	IsDraft          bool      `json:"isDraft"`
	Subject          string    `json:"subject"`
	BodyPreview      string    `json:"bodyPreview"`
	ReceivedDateTime time.Time `json:"receivedDateTime"`
	SentDateTime     time.Time `json:"sentDateTime"`
	IsRead           bool      `json:"isRead"`
	Flag             struct {
		FlagStatus string `json:"flagStatus"`
	} `json:"flag"`
	HasAttachments    bool   `json:"hasAttachments"`
	ParentFolderID    string `json:"parentFolderId"`
	InternetMessageID string `json:"internetMessageId"`
	Headers           []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"internetMessageHeaders"`
	Body struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
}

func (m *gMessage) summary() cm.Summary {
	s := cm.Summary{
		ID: m.ID, ThreadID: m.ConversationID, To: addrs(m.To), Subject: m.Subject,
		Snippet: strings.Join(strings.Fields(m.BodyPreview), " "), Date: m.ReceivedDateTime,
		Unread: !m.IsRead, Starred: m.Flag.FlagStatus == "flagged", HasAttachments: m.HasAttachments,
	}
	if s.Date.IsZero() {
		s.Date = m.SentDateTime
	}
	if m.From != nil {
		s.From = m.From.addr()
	}
	if m.IsDraft {
		s.DraftID = m.ID
	}
	return s
}

func (c *Client) List(ctx context.Context, f cm.Folder, query, cursor string, max int) (cm.Page, error) {
	var u string
	switch {
	case cursor != "":
		// The cursor is Graph's own next link. Only follow it to Graph, so
		// the token can't be sent anywhere else.
		if !strings.HasPrefix(cursor, c.Base+"/") {
			return cm.Page{}, errors.New("bad page cursor")
		}
		u = cursor
	case query != "":
		// $search can't be combined with $orderby; Graph returns results
		// by relevance and the app sorts them by date.
		u = fmt.Sprintf("%s/me/messages?$top=%d&$select=%s&$search=%s", c.Base, max, listFields,
			url.QueryEscape(`"`+strings.ReplaceAll(query, `"`, "")+`"`))
	case f == cm.Starred:
		u = fmt.Sprintf("%s/me/messages?$top=%d&$select=%s&$filter=%s", c.Base, max, listFields,
			url.QueryEscape("flag/flagStatus eq 'flagged'"))
	default:
		wk, ok := wellKnown[f]
		if !ok {
			return cm.Page{}, fmt.Errorf("unknown folder %q", f)
		}
		u = fmt.Sprintf("%s/me/mailFolders/%s/messages?$top=%d&$select=%s&$orderby=%s", c.Base, wk, max, listFields,
			url.QueryEscape("receivedDateTime desc"))
	}
	var r struct {
		Value    []gMessage `json:"value"`
		NextLink string     `json:"@odata.nextLink"`
	}
	if err := c.do(ctx, "GET", u, nil, &r); err != nil {
		var apiErr *oauth.APIError
		if f == cm.Archive && errors.As(err, &apiErr) && apiErr.Status == 404 {
			return cm.Page{}, nil // this mailbox has no Archive folder yet
		}
		return cm.Page{}, err
	}
	out := make([]cm.Summary, 0, len(r.Value))
	for i := range r.Value {
		out = append(out, r.Value[i].summary())
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	return cm.Page{Messages: out, Next: r.NextLink}, nil
}

func (c *Client) msgURL(id string) string {
	return c.Base + "/me/messages/" + url.PathEscape(id)
}

const fullFields = listFields + ",ccRecipients,bccRecipients,replyTo,body,internetMessageId,internetMessageHeaders,parentFolderId,isDraft"

func (c *Client) Get(ctx context.Context, id string) (*cm.Message, error) {
	var g gMessage
	if err := c.do(ctx, "GET", c.msgURL(id)+"?$select="+fullFields, nil, &g); err != nil {
		return nil, err
	}
	return c.full(ctx, &g)
}

// Thread returns a conversation's messages, oldest first.
func (c *Client) Thread(ctx context.Context, threadID string) ([]*cm.Message, error) {
	u := fmt.Sprintf("%s/me/messages?$top=50&$select=%s&$filter=%s&$orderby=%s", c.Base, fullFields,
		url.QueryEscape("conversationId eq '"+strings.ReplaceAll(threadID, "'", "''")+"'"),
		url.QueryEscape("receivedDateTime asc"))
	var r struct {
		Value []gMessage `json:"value"`
	}
	if err := c.do(ctx, "GET", u, nil, &r); err != nil {
		return nil, err
	}
	if len(r.Value) == 0 {
		return nil, fmt.Errorf("conversation not found")
	}
	out := make([]*cm.Message, 0, len(r.Value))
	for i := range r.Value {
		m, err := c.full(ctx, &r.Value[i])
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (c *Client) hidden(ctx context.Context, folderID string) bool {
	c.hiddenOnce.Do(func() {
		c.hiddenFolders = map[string]bool{}
		for _, wk := range []string{"deleteditems", "junkemail"} {
			var f struct {
				ID string `json:"id"`
			}
			if c.do(ctx, "GET", c.Base+"/me/mailFolders/"+wk+"?$select=id", nil, &f) == nil && f.ID != "" {
				c.hiddenFolders[f.ID] = true
			}
		}
	})
	return c.hiddenFolders[folderID]
}

func (c *Client) full(ctx context.Context, g *gMessage) (*cm.Message, error) {
	id := g.ID
	m := &cm.Message{
		Summary: g.summary(), Cc: addrs(g.Cc), Bcc: addrs(g.Bcc), ReplyTo: addrs(g.ReplyTo), MessageID: g.InternetMessageID,
	}
	if g.ParentFolderID != "" {
		m.Hidden = c.hidden(ctx, g.ParentFolderID)
	}
	if strings.EqualFold(g.Body.ContentType, "html") {
		m.HTML = g.Body.Content
	} else {
		m.Text = g.Body.Content
	}
	for _, h := range g.Headers {
		switch strings.ToLower(h.Name) {
		case "references":
			m.References = h.Value
		case "in-reply-to":
			m.InReplyTo = h.Value
		case "delivered-to", "x-original-to":
			m.DeliveredTo = append(m.DeliveredTo, cm.Address{Email: strings.Trim(h.Value, "<> ")})
		}
	}
	if g.HasAttachments || strings.Contains(m.HTML, "cid:") {
		var r struct {
			Value []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				ContentType string `json:"contentType"`
				Size        int64  `json:"size"`
				IsInline    bool   `json:"isInline"`
				ContentID   string `json:"contentId"`
			} `json:"value"`
		}
		if err := c.do(ctx, "GET", c.msgURL(id)+"/attachments", nil, &r); err != nil {
			return nil, err
		}
		for _, a := range r.Value {
			m.Attachments = append(m.Attachments, cm.Attachment{
				ID: a.ID, Name: a.Name, MIMEType: a.ContentType, Size: a.Size,
				ContentID: strings.Trim(a.ContentID, "<> "), Inline: a.IsInline,
			})
		}
	}
	return m, nil
}

func (c *Client) Attachment(ctx context.Context, msgID, attID string) ([]byte, error) {
	var b []byte
	err := c.do(ctx, "GET", c.msgURL(msgID)+"/attachments/"+url.PathEscape(attID)+"/$value", nil, &b)
	return b, err
}

func (c *Client) Apply(ctx context.Context, id string, ch cm.Change) error {
	patch := func(v map[string]any) error { return c.do(ctx, "PATCH", c.msgURL(id), v, nil) }
	move := func(dest string) error {
		return c.do(ctx, "POST", c.msgURL(id)+"/move", map[string]string{"destinationId": dest}, nil)
	}
	switch ch {
	case cm.MarkRead:
		return patch(map[string]any{"isRead": true})
	case cm.MarkUnread:
		return patch(map[string]any{"isRead": false})
	case cm.Star:
		return patch(map[string]any{"flag": map[string]string{"flagStatus": "flagged"}})
	case cm.Unstar:
		return patch(map[string]any{"flag": map[string]string{"flagStatus": "notFlagged"}})
	case cm.MoveArchive:
		return move("archive")
	case cm.MoveTrash:
		return move("deleteditems")
	case cm.MoveInbox:
		return move("inbox")
	case cm.MoveSpam:
		return move("junkemail")
	}
	return fmt.Errorf("unknown change %q", ch)
}

// Send delivers over SMTP. If the organization has SMTP turned off and the
// message is from the main address, it falls back to Graph, which can send
// as the main address only.
func (c *Client) Send(ctx context.Context, o *cm.Outgoing) error {
	err := c.sendSMTP(ctx, o)
	if err == nil {
		return nil
	}
	var smtpOff *SMTPDisabledError
	if !errors.As(err, &smtpOff) {
		return err
	}
	if !cm.SameEmail(o.From.Email, c.Email) {
		return fmt.Errorf("Microsoft turned down sending from %s over SMTP (%s). On a work account, ask your admin to allow SMTP AUTH for your mailbox and sending from aliases. Personal accounts can only send from aliases added to the account", o.From.Email, smtpOff.Msg)
	}
	raw, berr := cm.Build(o, true, time.Now())
	if berr != nil {
		return berr
	}
	req, rerr := http.NewRequestWithContext(ctx, "POST", c.Base+"/me/sendMail", strings.NewReader(base64.StdEncoding.EncodeToString(raw)))
	if rerr != nil {
		return rerr
	}
	req.Header.Set("Content-Type", "text/plain")
	return c.Src.Do(req, nil, nil)
}

func gaddrs(l []cm.Address) []map[string]any {
	out := make([]map[string]any, 0, len(l))
	for _, a := range l {
		out = append(out, map[string]any{"emailAddress": map[string]string{"name": a.Name, "address": a.Email}})
	}
	return out
}

// SaveDraft creates or updates a draft in the Drafts folder. A reply or
// forward is started from the original so Outlook keeps it in the
// conversation. Attachments are added for new drafts only, and only ones
// under 3 MB; larger files stay in the compose window.
func (c *Client) SaveDraft(ctx context.Context, draftID string, o *cm.Outgoing) (string, error) {
	fields := map[string]any{
		"subject":       o.Subject,
		"body":          map[string]string{"contentType": "html", "content": o.HTML},
		"toRecipients":  gaddrs(o.To),
		"ccRecipients":  gaddrs(o.Cc),
		"bccRecipients": gaddrs(o.Bcc),
		"from":          map[string]any{"emailAddress": map[string]string{"name": o.From.Name, "address": o.From.Email}},
	}
	var created struct {
		ID string `json:"id"`
	}
	if draftID == "" {
		switch {
		case o.OrigID != "" && (o.Kind == "reply" || o.Kind == "replyAll" || o.Kind == "forward"):
			verb := map[string]string{"reply": "createReply", "replyAll": "createReplyAll", "forward": "createForward"}[o.Kind]
			if err := c.do(ctx, "POST", c.msgURL(o.OrigID)+"/"+verb, map[string]any{}, &created); err != nil {
				return "", err
			}
			if err := c.do(ctx, "PATCH", c.msgURL(created.ID), fields, nil); err != nil {
				return "", err
			}
		default:
			if err := c.do(ctx, "POST", c.Base+"/me/messages", fields, &created); err != nil {
				return "", err
			}
		}
		draftID = created.ID
		for _, f := range o.Files {
			if len(f.Data) > 3<<20 {
				continue
			}
			att := map[string]any{"@odata.type": "#microsoft.graph.fileAttachment", "name": f.Name, "contentType": f.MIMEType,
				"contentBytes": base64.StdEncoding.EncodeToString(f.Data)}
			if err := c.do(ctx, "POST", c.msgURL(draftID)+"/attachments", att, nil); err != nil {
				return "", err
			}
		}
		return draftID, nil
	}
	if err := c.do(ctx, "PATCH", c.msgURL(draftID), fields, nil); err != nil {
		return "", err
	}
	return draftID, nil
}

func (c *Client) DeleteDraft(ctx context.Context, draftID string) error {
	err := c.do(ctx, "DELETE", c.msgURL(draftID), nil, nil)
	var apiErr *oauth.APIError
	if errors.As(err, &apiErr) && apiErr.Status == 404 {
		return nil
	}
	return err
}

// Aliases returns the main address plus, on work accounts, the other
// addresses on the mailbox. Graph can't list a personal account's aliases;
// those are added by hand.
func (c *Client) Aliases(ctx context.Context) ([]cm.Alias, error) {
	email, name, err := c.Me(ctx)
	if err != nil {
		return nil, err
	}
	out := []cm.Alias{{Email: email, Name: name, Primary: true, Default: true}}
	var r struct {
		ProxyAddresses []string `json:"proxyAddresses"`
	}
	if c.do(ctx, "GET", c.Base+"/me?$select=proxyAddresses", nil, &r) == nil {
		for _, p := range r.ProxyAddresses {
			addr, ok := strings.CutPrefix(strings.ToLower(p), "smtp:")
			if !ok || cm.SameEmail(addr, email) || strings.HasSuffix(addr, ".onmicrosoft.com") {
				continue
			}
			out = append(out, cm.Alias{Email: addr, Name: name})
		}
	}
	return out, nil
}

func (c *Client) Unread(ctx context.Context) (int, error) {
	var r struct {
		UnreadItemCount int `json:"unreadItemCount"`
	}
	err := c.do(ctx, "GET", c.Base+"/me/mailFolders/inbox?$select=unreadItemCount", nil, &r)
	return r.UnreadItemCount, err
}

// personalDomains are Microsoft's consumer mail domains, which use the
// Outlook.com SMTP server.
var personalDomains = []string{"outlook.", "hotmail.", "live.", "msn.", "passport."}

func smtpFor(email string) SMTPConfig {
	domain := strings.ToLower(email[strings.LastIndexByte(email, '@')+1:])
	for _, p := range personalDomains {
		if strings.HasPrefix(domain, p) {
			return SMTPConfig{Addr: "smtp-mail.outlook.com:587"}
		}
	}
	return SMTPConfig{Addr: "smtp.office365.com:587"}
}
