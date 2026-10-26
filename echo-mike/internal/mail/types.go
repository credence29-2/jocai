// Package mail holds the provider-neutral mail types, the MIME builder and the
// rules for picking a From address and signature.
package mail

import (
	"context"
	"errors"
	"time"
)

// Folder is one of the unified folders every provider maps onto.
type Folder string

const (
	Inbox   Folder = "inbox"
	Starred Folder = "starred"
	Sent    Folder = "sent"
	Archive Folder = "archive"
	Spam    Folder = "spam"
	Trash   Folder = "trash"
	Drafts  Folder = "drafts"
	// All is used for searches, which look everywhere except spam and trash.
	All Folder = "all"
)

// Folders lists the folders shown in the sidebar, in order.
var Folders = []Folder{Inbox, Starred, Sent, Drafts, Archive, Spam, Trash}

// Address is one mailbox, like "Jacob <jacob@example.com>".
type Address struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

// Summary is one row in the message list.
type Summary struct {
	AccountID      string    `json:"accountId"`
	ID             string    `json:"id"`
	ThreadID       string    `json:"threadId,omitempty"`
	From           Address   `json:"from"`
	To             []Address `json:"to,omitempty"`
	Subject        string    `json:"subject"`
	Snippet        string    `json:"snippet"`
	Date           time.Time `json:"date"`
	Unread         bool      `json:"unread"`
	Starred        bool      `json:"starred"`
	HasAttachments bool      `json:"hasAttachments"`
	// Hidden means the message is in Trash or Spam, so a thread opened from
	// another folder leaves it out.
	Hidden bool `json:"hidden,omitempty"`
	// Count and Participants are set on a row that stands for a whole
	// thread: how many messages it has and who wrote them.
	Count        int      `json:"count,omitempty"`
	Participants []string `json:"participants,omitempty"`
	// DraftID is set on a draft: the id to save or delete it by.
	DraftID string `json:"draftId,omitempty"`
	// Labels holds the ids of the user's own labels on the message (Gmail).
	Labels []string `json:"labels,omitempty"`
}

// Label is one of the user's own Gmail labels, or a custom Outlook folder.
type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// LabelPrefix turns a label id into a Folder: List("label:Label_12", ...).
const LabelPrefix = "label:"

// LabelFolder is the folder name that lists one label.
func LabelFolder(id string) Folder { return Folder(LabelPrefix + id) }

// LabelID returns the label behind a folder name, if it is one.
func LabelID(f Folder) (string, bool) {
	if len(f) > len(LabelPrefix) && string(f[:len(LabelPrefix)]) == LabelPrefix {
		return string(f[len(LabelPrefix):]), true
	}
	return "", false
}

// Labeler is a provider with labels or custom folders.
type Labeler interface {
	Labels(ctx context.Context) ([]Label, error)
	// SetLabel adds or removes a label on a message. For Outlook, on moves
	// the message into the folder and off moves it back to the inbox.
	SetLabel(ctx context.Context, id, labelID string, on bool) error
}

// Attachment describes a file on a received message.
type Attachment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MIMEType  string `json:"mimeType"`
	Size      int64  `json:"size"`
	ContentID string `json:"contentId,omitempty"`
	Inline    bool   `json:"inline,omitempty"`
}

// Message is a full message opened in the reading pane.
type Message struct {
	Summary
	Cc      []Address `json:"cc,omitempty"`
	Bcc     []Address `json:"bcc,omitempty"`
	ReplyTo []Address `json:"replyTo,omitempty"`
	// DeliveredTo holds any extra addresses the provider says this copy was
	// delivered to (Gmail's Delivered-To, X-Original-To). It helps pick the
	// alias a message was sent to when it came in through Bcc or a forward.
	DeliveredTo []Address    `json:"deliveredTo,omitempty"`
	MessageID   string       `json:"messageId,omitempty"`
	InReplyTo   string       `json:"inReplyTo,omitempty"`
	References  string       `json:"references,omitempty"`
	HTML        string       `json:"html,omitempty"`
	Text        string       `json:"text,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	// Invite is set when the message carries a calendar invitation.
	Invite *Invite `json:"invite,omitempty"`
}

// OutFile is an attachment on an outgoing message.
type OutFile struct {
	Name     string `json:"name"`
	MIMEType string `json:"mimeType"`
	Data     []byte `json:"data"`
}

// Outgoing is a message ready to send.
type Outgoing struct {
	From    Address
	ReplyTo string
	To      []Address
	Cc      []Address
	Bcc     []Address
	Subject string
	HTML    string
	Files   []OutFile
	// InReplyTo and References thread a reply; ThreadID keeps it in the same
	// Gmail conversation.
	InReplyTo  string
	References string
	ThreadID   string
	// OrigID is the provider's id of the message being replied to or
	// forwarded, and Kind is reply, replyAll or forward. Outlook drafts use
	// them to stay in the conversation.
	OrigID string
	Kind   string
	// ICS, when set, is a text/calendar part sent alongside the body: a
	// reply to an invitation.
	ICS       string
	ICSMethod string
}

// Alias is a send-as address a provider reports for an account.
type Alias struct {
	Email         string `json:"email"`
	Name          string `json:"name,omitempty"`
	ReplyTo       string `json:"replyTo,omitempty"`
	SignatureHTML string `json:"signatureHtml,omitempty"`
	Primary       bool   `json:"primary,omitempty"`
	Default       bool   `json:"default,omitempty"`
}

// Page is one page of a folder or search from one account.
type Page struct {
	Messages []Summary
	// Next is passed back to get the following page. Empty means no more.
	Next string
}

// Change is something to do to a message.
type Change string

const (
	MarkRead    Change = "read"
	MarkUnread  Change = "unread"
	Star        Change = "star"
	Unstar      Change = "unstar"
	MoveArchive Change = "archive"
	MoveTrash   Change = "trash"
	MoveInbox   Change = "inbox"
	MoveSpam    Change = "spam"
)

// Provider is one connected mailbox.
type Provider interface {
	// List returns a page of a folder, or of a search when query isn't empty.
	List(ctx context.Context, folder Folder, query, cursor string, max int) (Page, error)
	Get(ctx context.Context, id string) (*Message, error)
	// Thread returns every message in a conversation, oldest first.
	Thread(ctx context.Context, threadID string) ([]*Message, error)
	Apply(ctx context.Context, id string, c Change) error
	// Attachment returns the bytes of an attachment on a message.
	Attachment(ctx context.Context, msgID, attID string) ([]byte, error)
	Send(ctx context.Context, m *Outgoing) error
	// SaveDraft stores m as a draft, replacing draftID when it's set, and
	// returns the draft's id.
	SaveDraft(ctx context.Context, draftID string, m *Outgoing) (string, error)
	DeleteDraft(ctx context.Context, draftID string) error
	// Respond answers the invitation in message msgID as me, with status
	// ACCEPTED, TENTATIVE or DECLINED.
	Respond(ctx context.Context, msgID string, inv *Invite, me Address, status string) error
	// Aliases returns the send-as addresses the provider knows about. It
	// always includes the primary address.
	Aliases(ctx context.Context) ([]Alias, error)
	// Unread counts unread messages in the inbox.
	Unread(ctx context.Context) (int, error)
}

// ErrAuth means the account's sign-in has expired or was revoked and the
// person needs to reconnect it.
var ErrAuth = errors.New("sign-in expired, reconnect this account")
