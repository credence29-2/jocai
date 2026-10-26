package app

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

// MCPServer exposes the mailboxes to an MCP client such as Claude Desktop:
// list and read mail, search, and save drafts. It never sends; drafts are
// sent from the app. Run with ProjectEchoMike.exe -mcp over stdio.
func (a *App) MCPServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "project-echo-mike", Version: Version}, &mcp.ServerOptions{
		Instructions: "Project Echo Mike holds the user's Gmail and Outlook accounts. Use list_accounts to see accounts and the From addresses they can send as, list_mail to browse a folder or search, read_mail for a message or whole conversation, and draft_mail to leave a draft the user can review and send from the app. Nothing here sends mail.",
	})
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mcp.AddTool(s, &mcp.Tool{Name: "list_accounts", Description: "The connected mail accounts and the From addresses (aliases) each one can send as.", Annotations: ro}, a.mcpAccounts)
	mcp.AddTool(s, &mcp.Tool{Name: "list_mail", Description: "List a folder across every account, or one account, newest first. query searches like Gmail: words, from:, to:, subject:, has:attachment, after:2026-01-31, before:2026-02-01.", Annotations: ro}, a.mcpList)
	mcp.AddTool(s, &mcp.Tool{Name: "read_mail", Description: "Read one message as plain text, or the whole conversation it belongs to.", Annotations: ro}, a.mcpRead)
	mcp.AddTool(s, &mcp.Tool{Name: "draft_mail", Description: "Save a draft in the right account, as a new message or a reply to one, for the user to review and send from Project Echo Mike. Writes plain paragraphs; the From address's signature is added.", Annotations: &mcp.ToolAnnotations{IdempotentHint: false}}, a.mcpDraft)
	return s
}

// ServeMCP runs the server over stdin and stdout until the client goes away.
func (a *App) ServeMCP(ctx context.Context) error {
	return a.MCPServer().Run(ctx, &mcp.StdioTransport{})
}

type mcpAccountsOut struct {
	Accounts []struct {
		ID       string   `json:"id"`
		Provider string   `json:"provider"`
		Email    string   `json:"email"`
		Name     string   `json:"name,omitempty"`
		SendAs   []string `json:"sendAs"`
	} `json:"accounts"`
}

func (a *App) mcpAccounts(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, mcpAccountsOut, error) {
	d := a.st.Get()
	var out mcpAccountsOut
	var text strings.Builder
	for _, ac := range d.Accounts {
		row := struct {
			ID       string   `json:"id"`
			Provider string   `json:"provider"`
			Email    string   `json:"email"`
			Name     string   `json:"name,omitempty"`
			SendAs   []string `json:"sendAs"`
		}{ID: ac.ID, Provider: ac.Provider, Email: ac.Email, Name: ac.Name, SendAs: []string{}}
		for _, id := range d.Identities {
			if id.AccountID == ac.ID {
				row.SendAs = append(row.SendAs, id.Email)
			}
		}
		out.Accounts = append(out.Accounts, row)
		fmt.Fprintf(&text, "%s (%s, id %s): sends as %s\n", ac.Email, ac.Provider, ac.ID, strings.Join(row.SendAs, ", "))
	}
	if len(out.Accounts) == 0 {
		text.WriteString("No accounts are connected yet. Add one in Project Echo Mike under Settings > Accounts.")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text.String()}}}, out, nil
}

type mcpListIn struct {
	Folder    string `json:"folder,omitempty" jsonschema:"inbox (default), starred, sent, drafts, archive, spam or trash"`
	AccountID string `json:"accountId,omitempty" jsonschema:"one account's id from list_accounts; empty for all"`
	Query     string `json:"query,omitempty" jsonschema:"search words and operators; searches all mail, not just the folder"`
	Limit     int    `json:"limit,omitempty" jsonschema:"rows to return, default 25, max 100"`
}

type mcpRow struct {
	AccountID   string `json:"accountId"`
	ID          string `json:"id"`
	ThreadID    string `json:"threadId,omitempty"`
	From        string `json:"from"`
	Subject     string `json:"subject"`
	Date        string `json:"date"`
	Snippet     string `json:"snippet"`
	Unread      bool   `json:"unread"`
	Attachments bool   `json:"attachments"`
	Count       int    `json:"messages,omitempty"`
}

func (a *App) mcpList(ctx context.Context, req *mcp.CallToolRequest, in mcpListIn) (*mcp.CallToolResult, map[string][]mcpRow, error) {
	folder := mail.Folder(strings.ToLower(strings.TrimSpace(in.Folder)))
	if folder == "" {
		folder = mail.Inbox
	}
	known := false
	for _, f := range mail.Folders {
		if f == folder {
			known = true
		}
	}
	if !known {
		return nil, nil, fmt.Errorf("unknown folder %q", in.Folder)
	}
	out, err := a.list(ctx, listIn{Folder: folder, Query: in.Query, AccountID: in.AccountID, Group: true})
	if err != nil {
		return nil, nil, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if len(out.Messages) > limit {
		out.Messages = out.Messages[:limit]
	}
	var rows []mcpRow
	var text strings.Builder
	for _, m := range out.Messages {
		r := mcpRow{AccountID: m.AccountID, ID: m.ID, ThreadID: m.ThreadID, From: addrLine([]mail.Address{m.From}), Subject: m.Subject,
			Date: m.Date.Local().Format("2006-01-02 15:04"), Snippet: m.Snippet, Unread: m.Unread, Attachments: m.HasAttachments, Count: m.Count}
		rows = append(rows, r)
		flags := ""
		if m.Unread {
			flags += " [unread]"
		}
		if m.HasAttachments {
			flags += " [attachment]"
		}
		if m.Count > 1 {
			flags += fmt.Sprintf(" [%d messages]", m.Count)
		}
		fmt.Fprintf(&text, "%s  %s  %s%s\n  account %s, id %s, thread %s\n  %s\n", r.Date, r.From, r.Subject, flags, m.AccountID, m.ID, m.ThreadID, m.Snippet)
	}
	for id, e := range out.Errors {
		fmt.Fprintf(&text, "Account %s couldn't be read: %s\n", id, e)
	}
	if len(rows) == 0 && len(out.Errors) == 0 {
		text.WriteString("Nothing there.")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text.String()}}}, map[string][]mcpRow{"messages": rows}, nil
}

type mcpReadIn struct {
	AccountID string `json:"accountId" jsonschema:"the account id from list_mail"`
	ID        string `json:"id,omitempty" jsonschema:"the message id from list_mail"`
	ThreadID  string `json:"threadId,omitempty" jsonschema:"set this instead of id to read the whole conversation"`
}

func (a *App) mcpRead(ctx context.Context, req *mcp.CallToolRequest, in mcpReadIn) (*mcp.CallToolResult, any, error) {
	text, err := a.mailText(ctx, in.AccountID, in.ID, in.ThreadID)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

type mcpDraftIn struct {
	From      string `json:"from" jsonschema:"the address to send as, one of the sendAs addresses from list_accounts"`
	To        string `json:"to,omitempty" jsonschema:"recipients, comma separated; filled in from the original on a reply"`
	Cc        string `json:"cc,omitempty"`
	Subject   string `json:"subject,omitempty" jsonschema:"filled in from the original on a reply"`
	Body      string `json:"body" jsonschema:"the message as plain text, paragraphs separated by blank lines, without a signature"`
	AccountID string `json:"replyAccountId,omitempty" jsonschema:"to reply: the account id of the message being answered"`
	ReplyToID string `json:"replyToId,omitempty" jsonschema:"to reply: the id of the message being answered"`
	ReplyAll  bool   `json:"replyAll,omitempty"`
}

func (a *App) mcpDraft(ctx context.Context, req *mcp.CallToolRequest, in mcpDraftIn) (*mcp.CallToolResult, map[string]string, error) {
	d := a.st.Get()
	var ident *mail.Identity
	for i := range d.Identities {
		if mail.SameEmail(d.Identities[i].Email, in.From) {
			ident = &d.Identities[i]
			break
		}
	}
	if ident == nil {
		return nil, nil, fmt.Errorf("%q isn't an address this app can send as; see list_accounts", in.From)
	}
	if strings.TrimSpace(in.Body) == "" {
		return nil, nil, fmt.Errorf("the body is empty")
	}
	o := &mail.Outgoing{From: mail.Address{Name: displayName(ident), Email: ident.Email}, ReplyTo: ident.ReplyTo, Subject: in.Subject}
	quote := ""
	if in.ReplyToID != "" {
		src, _, err := a.provider(in.AccountID)
		if err != nil {
			return nil, nil, err
		}
		orig, err := src.Get(ctx, in.ReplyToID)
		if err != nil {
			a.noteErr(in.AccountID, err)
			return nil, nil, err
		}
		kind := mail.Reply
		if in.ReplyAll {
			kind = mail.ReplyAll
		}
		rd := mail.MakeDraft(kind, d.Identities, in.AccountID, orig)
		o.InReplyTo, o.References, o.Kind = rd.InReplyTo, rd.References, string(kind)
		if in.AccountID == ident.AccountID {
			o.ThreadID, o.OrigID = rd.ThreadID, in.ReplyToID
		}
		if o.Subject == "" {
			o.Subject = rd.Subject
		}
		if strings.TrimSpace(in.To) == "" {
			o.To = rd.To
			o.Cc = rd.Cc
		}
		quote = "<br>" + rd.QuoteHTML
	}
	var err error
	if strings.TrimSpace(in.To) != "" {
		if o.To, err = mail.ParseAddresses(in.To); err != nil {
			return nil, nil, fmt.Errorf("To: %w", err)
		}
	}
	if strings.TrimSpace(in.Cc) != "" {
		if o.Cc, err = mail.ParseAddresses(in.Cc); err != nil {
			return nil, nil, fmt.Errorf("Cc: %w", err)
		}
	}
	sig := ""
	if ident.SignatureHTML != "" && (in.ReplyToID == "" || ident.SignatureOnReplies) {
		sig = `<div><br></div><div class="cm-signature">` + ident.SignatureHTML + "</div>"
	}
	o.HTML = textHTML(in.Body) + sig + quote
	p, _, err := a.provider(ident.AccountID)
	if err != nil {
		return nil, nil, err
	}
	id, err := p.SaveDraft(ctx, "", o)
	if err != nil {
		a.noteErr(ident.AccountID, err)
		return nil, nil, err
	}
	msg := fmt.Sprintf("Draft saved in %s's Drafts folder from %s, subject %q. The user can open it under Drafts in Project Echo Mike and send it.", ident.AccountID, ident.Email, o.Subject)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: msg}}}, map[string]string{"draftId": id, "accountId": ident.AccountID}, nil
}

// textHTML turns plain paragraphs into the editor's HTML.
func textHTML(t string) string {
	var b strings.Builder
	for i, para := range strings.Split(strings.ReplaceAll(strings.TrimSpace(t), "\r\n", "\n"), "\n\n") {
		if i > 0 {
			b.WriteString("<div><br></div>")
		}
		b.WriteString("<div>" + strings.ReplaceAll(html.EscapeString(strings.TrimSpace(para)), "\n", "<br>") + "</div>")
	}
	return b.String()
}
