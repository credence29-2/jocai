// Package app is Project Echo Mike itself: the local server the window talks
// to, account sign-in, and the new-mail watcher. The Windows and Android apps
// are thin shells around it.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/credence29-2/jocai/echo-mike/internal/demo"
	"github.com/credence29-2/jocai/echo-mike/internal/mail"
	"github.com/credence29-2/jocai/echo-mike/internal/oauth"
	"github.com/credence29-2/jocai/echo-mike/internal/store"
	"github.com/credence29-2/jocai/echo-mike/internal/update"
	"github.com/credence29-2/jocai/echo-mike/web"
)

const Name = "Project Echo Mike"

// Version is set at build time with -ldflags "-X .../internal/app.Version=...".
var Version = "1.0.0"

// pageSize is how many messages each account returns per page.
const pageSize = 30

// maxSend caps a send request, attachments included.
const maxSend = 40 << 20

// app is the local HTTP server the window talks to. It listens on 127.0.0.1
// only, and every API call must carry the random token handed to the window,
// so other programs and web pages can't read mail through it.
type App struct {
	st    *store.Store
	token string
	// openURL opens a link in the system browser.
	openURL func(string) error
	demo    bool

	mu        sync.Mutex
	providers map[string]mail.Provider
	flow      *oauth.Flow

	// notify shows a desktop notification; nil where there's no desktop.
	Notify func(title, body string)
	// onNewMail tells the window to refresh; nil when there's no window.
	OnNewMail func()
	watch     watcher
	book      contacts

	// Updater checks for and installs new versions; nil where the app
	// can't replace itself (Android, development).
	Updater *update.Updater
	// Restart relaunches the program after an update is applied.
	Restart func()
}

func New(st *store.Store, demoMode bool, openURL func(string) error) *App {
	b := make([]byte, 24)
	rand.Read(b)
	a := &App{st: st, token: hex.EncodeToString(b), openURL: openURL, demo: demoMode, providers: map[string]mail.Provider{}}
	if demoMode {
		a.addDemoAccounts()
	}
	st.Migrate()
	return a
}

// addDemoAccounts puts two sample accounts in memory. Nothing is saved.
func (a *App) addDemoAccounts() {
	g, o := demo.Boxes(time.Now())
	a.providers["demo-g"] = g
	a.providers["demo-o"] = o
	a.st.Update(func(d *store.Data) error {
		d.Accounts = []store.Account{
			{ID: "demo-g", Provider: "gmail", Email: g.Email, Name: "Personal Gmail", Color: "#d93025"},
			{ID: "demo-o", Provider: "outlook", Email: o.Email, Name: "Outlook", Color: "#0a64d6"},
		}
		d.Identities = nil
		return nil
	})
	for _, id := range []string{"demo-g", "demo-o"} {
		a.importAliases(context.Background(), id)
	}
	a.st.Update(func(d *store.Data) error {
		for i := range d.Identities {
			d.Identities[i].SignatureOnReplies = true
		}
		return nil
	})
}

// listen starts serving on a random loopback port and returns the window URL.
func (a *App) Listen() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	srv := &http.Server{Handler: a.routes(), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	return fmt.Sprintf("http://%s/#%s", ln.Addr(), a.token), nil
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(web.Index)
	})
	api := func(pattern string, h func(w http.ResponseWriter, r *http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if !a.authorized(r) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			out, err := h(w, r)
			if out == nil && err == nil {
				return // the handler wrote its own response
			}
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				code := http.StatusInternalServerError
				if errors.Is(err, mail.ErrAuth) {
					code = http.StatusUnauthorized
				}
				w.WriteHeader(code)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(out)
		})
	}
	api("GET /api/state", a.handleState)
	api("POST /api/list", a.handleList)
	api("GET /api/message", a.handleMessage)
	api("GET /api/thread", a.handleThread)
	api("POST /api/apply", a.handleApply)
	api("GET /api/attachment", a.handleAttachment)
	api("POST /api/draft", a.handleDraft)
	api("POST /api/send", a.handleSend)
	api("POST /api/draft/save", a.handleDraftSave)
	api("POST /api/draft/delete", a.handleDraftDelete)
	api("POST /api/identity", a.handleIdentity)
	api("POST /api/identity/delete", a.handleIdentityDelete)
	api("POST /api/aliases/import", a.handleImport)
	api("POST /api/aliases/signature", a.handlePullSignature)
	api("POST /api/settings", a.handleSettings)
	api("POST /api/connect", a.handleConnect)
	api("POST /api/connect/cancel", a.handleConnectCancel)
	api("POST /api/account/update", a.handleAccountUpdate)
	api("POST /api/account/remove", a.handleAccountRemove)
	api("GET /api/unread", a.handleUnread)
	api("GET /api/contacts", a.handleContacts)
	api("POST /api/invite/respond", a.handleInviteRespond)
	api("GET /api/update", a.handleUpdateStatus)
	api("POST /api/update/check", a.handleUpdateCheck)
	api("POST /api/update/install", a.handleUpdateInstall)
	api("GET /api/cal/calendars", a.handleCalendars)
	api("GET /api/cal/events", a.handleEvents)
	api("POST /api/cal/event", a.handleEventSave)
	api("POST /api/cal/event/delete", a.handleEventDelete)
	api("POST /api/cal/event/respond", a.handleEventRespond)
	api("POST /api/ai/test", a.handleAITest)
	api("POST /api/ai/run", a.handleAIRun)
	api("GET /open", a.handleOpen)
	return a.guardHost(mux)
}

// guardHost refuses requests whose Host isn't the loopback address, which
// stops DNS rebinding tricks from a web page.
func (a *App) guardHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || (host != "127.0.0.1" && host != "localhost") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorized checks the token. API calls send it as a header, which a web
// page on another origin can't add without a CORS preflight we never allow.
// Links that open downloads or the browser carry it in the query instead.
func (a *App) authorized(r *http.Request) bool {
	t := r.Header.Get("X-Echo-Token")
	if t == "" && r.Method == http.MethodGet {
		t = r.URL.Query().Get("t")
	}
	return len(t) == len(a.token) && t == a.token
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxSend)).Decode(v); err != nil {
		return fmt.Errorf("bad request: %w", err)
	}
	return nil
}

// provider returns the live connection for an account, opening it if needed.
func (a *App) provider(accountID string) (mail.Provider, store.Account, error) {
	var acct store.Account
	found := false
	for _, ac := range a.st.Get().Accounts {
		if ac.ID == accountID {
			acct, found = ac, true
		}
	}
	if !found {
		return nil, acct, fmt.Errorf("account not found")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if p := a.providers[accountID]; p != nil {
		return p, acct, nil
	}
	p, err := a.openProvider(acct)
	if err != nil {
		return nil, acct, err
	}
	a.providers[accountID] = p
	return p, acct, nil
}

// noteErr flags an account for reconnecting when its sign-in stopped working.
func (a *App) noteErr(accountID string, err error) {
	if !errors.Is(err, mail.ErrAuth) {
		return
	}
	a.st.Update(func(d *store.Data) error {
		for i := range d.Accounts {
			if d.Accounts[i].ID == accountID {
				d.Accounts[i].NeedsReconnect = true
			}
		}
		return nil
	})
	a.mu.Lock()
	delete(a.providers, accountID)
	a.mu.Unlock()
}

type stateOut struct {
	Version    string          `json:"version"`
	Demo       bool            `json:"demo"`
	Accounts   []store.Account `json:"accounts"`
	Identities []mail.Identity `json:"identities"`
	Settings   store.Settings  `json:"settings"`
	Folders    []mail.Folder   `json:"folders"`
	// Updates says whether this build can update itself.
	Updates bool `json:"updates"`
	// Exe is this program's path, for the Claude Desktop setup snippet.
	Exe string `json:"exe,omitempty"`
}

func (a *App) handleState(w http.ResponseWriter, r *http.Request) (any, error) {
	d := a.st.Get()
	if d.Settings.GoogleClientSecret != "" {
		d.Settings.GoogleClientSecret = "saved"
	}
	if d.Settings.AnthropicKey != "" {
		d.Settings.AnthropicKey = "saved"
	}
	if d.Settings.OpenAIKey != "" {
		d.Settings.OpenAIKey = "saved"
	}
	if d.Accounts == nil {
		d.Accounts = []store.Account{}
	}
	if d.Identities == nil {
		d.Identities = []mail.Identity{}
	}
	exe, _ := os.Executable()
	return stateOut{Version: Version, Demo: a.demo, Accounts: d.Accounts, Identities: d.Identities, Settings: d.Settings, Folders: mail.Folders, Updates: a.Updater != nil, Exe: exe}, nil
}

type listIn struct {
	Folder    mail.Folder `json:"folder"`
	Query     string      `json:"query"`
	AccountID string      `json:"accountId"` // empty for every account
	// Cursors holds each account's next-page cursor. When it's set, only
	// accounts with a cursor are asked, for "load more".
	Cursors map[string]string `json:"cursors"`
	// Group folds each conversation into one row.
	Group bool `json:"group"`
}

type listOut struct {
	Messages []mail.Summary    `json:"messages"`
	Cursors  map[string]string `json:"cursors"`
	Errors   map[string]string `json:"errors"`
}

func (a *App) handleList(w http.ResponseWriter, r *http.Request) (any, error) {
	var in listIn
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return a.list(r.Context(), in)
}

// list asks every account (or one) for a page of a folder or search.
func (a *App) list(ctx context.Context, in listIn) (listOut, error) {
	var ids []string
	for _, ac := range a.st.Get().Accounts {
		if in.AccountID != "" && ac.ID != in.AccountID {
			continue
		}
		if in.Cursors != nil && in.Cursors[ac.ID] == "" {
			continue
		}
		ids = append(ids, ac.ID)
	}
	out := listOut{Messages: []mail.Summary{}, Cursors: map[string]string{}, Errors: map[string]string{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			p, _, err := a.provider(id)
			var page mail.Page
			if err == nil {
				page, err = p.List(ctx, in.Folder, in.Query, in.Cursors[id], pageSize)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				a.noteErr(id, err)
				out.Errors[id] = err.Error()
				return
			}
			for i := range page.Messages {
				page.Messages[i].AccountID = id
			}
			for _, m := range page.Messages {
				a.learn(1, []mail.Address{m.From}, m.To)
			}
			out.Messages = append(out.Messages, page.Messages...)
			if page.Next != "" {
				out.Cursors[id] = page.Next
			}
		}(id)
	}
	wg.Wait()
	a.saveContacts()
	if in.Group {
		out.Messages = groupThreads(out.Messages)
	}
	sort.SliceStable(out.Messages, func(i, j int) bool { return out.Messages[i].Date.After(out.Messages[j].Date) })
	return out, nil
}

// groupThreads folds messages of the same conversation into one row: the
// newest message, carrying the count, everyone who wrote, and whether any
// message is unread, starred or has a file.
func groupThreads(msgs []mail.Summary) []mail.Summary {
	type key struct{ acct, thread string }
	rows := map[key]*mail.Summary{}
	var order []key
	for _, m := range msgs {
		k := key{m.AccountID, m.ThreadID}
		if m.ThreadID == "" {
			k.thread = "msg:" + m.ID
		}
		r, ok := rows[k]
		if !ok {
			c := m
			c.Count = 1
			c.Participants = []string{who(m)}
			rows[k] = &c
			order = append(order, k)
			continue
		}
		r.Count++
		if !slices.Contains(r.Participants, who(m)) {
			r.Participants = append(r.Participants, who(m))
		}
		r.Unread = r.Unread || m.Unread
		r.Starred = r.Starred || m.Starred
		r.HasAttachments = r.HasAttachments || m.HasAttachments
		if m.Date.After(r.Date) {
			// Keep the newest message's subject, snippet and date, but
			// list writers oldest first like the conversation reads.
			parts, count := r.Participants, r.Count
			*r = m
			r.Participants, r.Count = parts, count
			r.Unread, r.Starred, r.HasAttachments = r.Unread || m.Unread, r.Starred || m.Starred, r.HasAttachments || m.HasAttachments
		}
	}
	out := make([]mail.Summary, 0, len(order))
	for _, k := range order {
		out = append(out, *rows[k])
	}
	return out
}

func who(m mail.Summary) string {
	if m.From.Name != "" {
		return m.From.Name
	}
	return m.From.Email
}

// handleThread returns every message in a conversation, oldest first.
func (a *App) handleThread(w http.ResponseWriter, r *http.Request) (any, error) {
	acct, id := r.URL.Query().Get("a"), r.URL.Query().Get("id")
	p, _, err := a.provider(acct)
	if err != nil {
		return nil, err
	}
	msgs, err := p.Thread(r.Context(), id)
	if err != nil {
		a.noteErr(acct, err)
		return nil, err
	}
	for _, m := range msgs {
		m.AccountID = acct
		a.learn(1, []mail.Address{m.From}, m.To, m.Cc)
	}
	a.saveContacts()
	return msgs, nil
}

func (a *App) handleMessage(w http.ResponseWriter, r *http.Request) (any, error) {
	acct, id := r.URL.Query().Get("a"), r.URL.Query().Get("id")
	p, _, err := a.provider(acct)
	if err != nil {
		return nil, err
	}
	m, err := p.Get(r.Context(), id)
	if err != nil {
		a.noteErr(acct, err)
		return nil, err
	}
	m.AccountID = acct
	return m, nil
}

func (a *App) handleApply(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		AccountID string   `json:"accountId"`
		IDs       []string `json:"ids"`
		// Threads applies the change to every message in each conversation.
		Threads []string    `json:"threads"`
		Change  mail.Change `json:"change"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	p, _, err := a.provider(in.AccountID)
	if err != nil {
		return nil, err
	}
	for _, t := range in.Threads {
		msgs, err := p.Thread(r.Context(), t)
		if err != nil {
			a.noteErr(in.AccountID, err)
			return nil, err
		}
		for _, m := range msgs {
			// Leave trashed or spam messages where they are unless the
			// change is putting things back in the inbox.
			if m.Hidden && in.Change != mail.MoveInbox {
				continue
			}
			in.IDs = append(in.IDs, m.ID)
		}
	}
	for _, id := range in.IDs {
		if err := p.Apply(r.Context(), id, in.Change); err != nil {
			a.noteErr(in.AccountID, err)
			return nil, err
		}
	}
	return map[string]bool{"ok": true}, nil
}

func (a *App) handleAttachment(w http.ResponseWriter, r *http.Request) (any, error) {
	q := r.URL.Query()
	p, _, err := a.provider(q.Get("a"))
	if err != nil {
		return nil, err
	}
	data, err := p.Attachment(r.Context(), q.Get("m"), q.Get("att"))
	if err != nil {
		return nil, err
	}
	typ := q.Get("type")
	disp := "attachment"
	// Inline images are shown in the message, and images and PDFs can be
	// previewed; everything else downloads.
	previewable := (strings.HasPrefix(typ, "image/") && typ != "image/svg+xml") || typ == "application/pdf"
	if q.Get("inline") == "1" && previewable {
		disp = "inline"
		// Nothing in a previewed file may load anything else. (A sandbox
		// directive would also stop the browser's PDF viewer.)
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	} else {
		typ = "application/octet-stream"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": q.Get("name")}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(data)
	return nil, nil
}

func (a *App) handleDraft(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		AccountID string         `json:"accountId"`
		ID        string         `json:"id"`
		Kind      mail.ReplyKind `json:"kind"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	p, _, err := a.provider(in.AccountID)
	if err != nil {
		return nil, err
	}
	m, err := p.Get(r.Context(), in.ID)
	if err != nil {
		return nil, err
	}
	return mail.MakeDraft(in.Kind, a.st.Get().Identities, in.AccountID, m), nil
}

type sendIn struct {
	IdentityID string         `json:"identityId"`
	To         string         `json:"to"`
	Cc         string         `json:"cc"`
	Bcc        string         `json:"bcc"`
	Subject    string         `json:"subject"`
	HTML       string         `json:"html"`
	Files      []mail.OutFile `json:"files"`
	// Forward lists attachments to copy from the original message.
	Forward []struct {
		AccountID string `json:"accountId"`
		MsgID     string `json:"msgId"`
		mail.Attachment
	} `json:"forward"`
	InReplyTo  string `json:"inReplyTo"`
	References string `json:"references"`
	ThreadID   string `json:"threadId"`
	// ReplyAccountID is the account the original arrived in. A Gmail thread
	// ID only means something in that account.
	ReplyAccountID string `json:"replyAccountId"`
	// OrigID is the original message's id in that account, and Kind is
	// reply, replyAll or forward.
	OrigID string `json:"origId"`
	Kind   string `json:"kind"`
	// DraftID and DraftAccountID name the saved draft this message came
	// from, if any. Sending deletes it; saving replaces it.
	DraftID        string `json:"draftId"`
	DraftAccountID string `json:"draftAccountId"`
}

// outgoing turns a compose request into a message, resolving the From
// identity and copying forwarded attachments.
func (a *App) outgoing(ctx context.Context, in *sendIn) (*mail.Outgoing, *mail.Identity, error) {
	var id *mail.Identity
	for _, x := range a.st.Get().Identities {
		if x.ID == in.IdentityID {
			x := x
			id = &x
		}
	}
	if id == nil {
		return nil, nil, fmt.Errorf("pick a From address")
	}
	o := &mail.Outgoing{
		From: mail.Address{Name: displayName(id), Email: id.Email}, ReplyTo: id.ReplyTo,
		Subject: in.Subject, HTML: in.HTML, Files: in.Files,
		InReplyTo: in.InReplyTo, References: in.References, Kind: in.Kind,
	}
	if in.ReplyAccountID == id.AccountID {
		o.ThreadID = in.ThreadID
		o.OrigID = in.OrigID
	}
	var err error
	for _, f := range []struct {
		dst *[]mail.Address
		src string
		lbl string
	}{{&o.To, in.To, "To"}, {&o.Cc, in.Cc, "Cc"}, {&o.Bcc, in.Bcc, "Bcc"}} {
		if *f.dst, err = mail.ParseAddresses(f.src); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", f.lbl, err)
		}
	}
	for _, fw := range in.Forward {
		src, _, err := a.provider(fw.AccountID)
		if err != nil {
			return nil, nil, err
		}
		data, err := src.Attachment(ctx, fw.MsgID, fw.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("couldn't copy %s from the original: %w", fw.Name, err)
		}
		o.Files = append(o.Files, mail.OutFile{Name: fw.Name, MIMEType: fw.MIMEType, Data: data})
	}
	return o, id, nil
}

// dropDraft deletes a draft after it was sent or replaced. Failing to is
// not worth failing the send over.
func (a *App) dropDraft(ctx context.Context, accountID, draftID string) {
	if draftID == "" {
		return
	}
	if p, _, err := a.provider(accountID); err == nil {
		p.DeleteDraft(ctx, draftID)
	}
}

// handleDraftSave stores the compose window as a draft and returns its id.
func (a *App) handleDraftSave(w http.ResponseWriter, r *http.Request) (any, error) {
	var in sendIn
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	o, id, err := a.outgoing(r.Context(), &in)
	if err != nil {
		return nil, err
	}
	p, _, err := a.provider(id.AccountID)
	if err != nil {
		return nil, err
	}
	draftID := in.DraftID
	if draftID != "" && in.DraftAccountID != id.AccountID {
		// The From address moved to another account: the draft moves too.
		a.dropDraft(r.Context(), in.DraftAccountID, draftID)
		draftID = ""
	}
	newID, err := p.SaveDraft(r.Context(), draftID, o)
	if err != nil {
		a.noteErr(id.AccountID, err)
		return nil, err
	}
	return map[string]string{"draftId": newID, "accountId": id.AccountID}, nil
}

func (a *App) handleDraftDelete(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		AccountID string `json:"accountId"`
		DraftID   string `json:"draftId"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	p, _, err := a.provider(in.AccountID)
	if err != nil {
		return nil, err
	}
	if err := p.DeleteDraft(r.Context(), in.DraftID); err != nil {
		a.noteErr(in.AccountID, err)
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

func (a *App) handleSend(w http.ResponseWriter, r *http.Request) (any, error) {
	var in sendIn
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	o, id, err := a.outgoing(r.Context(), &in)
	if err != nil {
		return nil, err
	}
	p, _, err := a.provider(id.AccountID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	if err := p.Send(ctx, o); err != nil {
		a.noteErr(id.AccountID, err)
		return nil, err
	}
	a.dropDraft(ctx, in.DraftAccountID, in.DraftID)
	a.learn(5, o.To, o.Cc, o.Bcc)
	a.saveContacts()
	return map[string]bool{"ok": true}, nil
}

func (a *App) handleIdentity(w http.ResponseWriter, r *http.Request) (any, error) {
	var in mail.Identity
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	in.Email = strings.TrimSpace(in.Email)
	if _, err := mail.ParseAddresses(in.Email); err != nil || !strings.Contains(in.Email, "@") {
		return nil, fmt.Errorf("%q isn't a valid email address", in.Email)
	}
	err := a.st.Update(func(d *store.Data) error {
		ok := false
		for _, ac := range d.Accounts {
			ok = ok || ac.ID == in.AccountID
		}
		if !ok {
			return fmt.Errorf("account not found")
		}
		if in.ID == "" {
			in.ID = store.NewID()
			d.Identities = append(d.Identities, in)
		} else {
			found := false
			for i := range d.Identities {
				if d.Identities[i].ID == in.ID {
					in.Imported = d.Identities[i].Imported
					d.Identities[i] = in
					found = true
				}
			}
			if !found {
				return fmt.Errorf("identity not found")
			}
		}
		// Only one default per account.
		hasDefault := false
		for i := range d.Identities {
			x := &d.Identities[i]
			if x.AccountID != in.AccountID {
				continue
			}
			if in.Default && x.ID != in.ID {
				x.Default = false
			}
			hasDefault = hasDefault || x.Default
		}
		if !hasDefault {
			for i := range d.Identities {
				if d.Identities[i].AccountID == in.AccountID {
					d.Identities[i].Default = true
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return in, nil
}

func (a *App) handleIdentityDelete(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	err := a.st.Update(func(d *store.Data) error {
		var acct string
		kept := d.Identities[:0]
		for _, x := range d.Identities {
			if x.ID == in.ID {
				acct = x.AccountID
				continue
			}
			kept = append(kept, x)
		}
		left := 0
		for _, x := range kept {
			if x.AccountID == acct {
				left++
			}
		}
		if acct != "" && left == 0 {
			return fmt.Errorf("each account needs at least one From address")
		}
		d.Identities = kept
		if d.Settings.DefaultIdentity == in.ID {
			d.Settings.DefaultIdentity = ""
		}
		return nil
	})
	return map[string]bool{"ok": err == nil}, err
}

// handlePullSignature fetches one address's current signature from the
// provider without saving it, so the alias editor can show it before Save.
func (a *App) handlePullSignature(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		AccountID string `json:"accountId"`
		Email     string `json:"email"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	p, _, err := a.provider(in.AccountID)
	if err != nil {
		return nil, err
	}
	aliases, err := p.Aliases(r.Context())
	if err != nil {
		a.noteErr(in.AccountID, err)
		return nil, err
	}
	for _, al := range aliases {
		if !strings.EqualFold(al.Email, strings.TrimSpace(in.Email)) {
			continue
		}
		if strings.TrimSpace(al.SignatureHTML) == "" {
			return nil, fmt.Errorf("Gmail has no default signature for %s. In Gmail settings, under Signature defaults, pick %s and choose a signature for new emails, then save and try again", al.Email, al.Email)
		}
		return al, nil
	}
	return nil, fmt.Errorf("Gmail doesn't list %s as a verified send-as address on this account", in.Email)
}

func (a *App) handleImport(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		AccountID string `json:"accountId"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	n, err := a.importAliases(r.Context(), in.AccountID)
	return map[string]int{"added": n}, err
}

// importAliases adds the provider's send-as addresses as identities. Ones
// already there keep the person's edits; only blank fields are filled in.
func (a *App) importAliases(ctx context.Context, accountID string) (int, error) {
	p, acct, err := a.provider(accountID)
	if err != nil {
		return 0, err
	}
	aliases, err := p.Aliases(ctx)
	if err != nil {
		a.noteErr(accountID, err)
		return 0, err
	}
	added := 0
	err = a.st.Update(func(d *store.Data) error {
		for _, al := range aliases {
			var have *mail.Identity
			for i := range d.Identities {
				x := &d.Identities[i]
				if x.AccountID == accountID && strings.EqualFold(x.Email, al.Email) {
					have = x
				}
			}
			if have != nil {
				have.Imported = true
				if have.Name == "" {
					have.Name = al.Name
				}
				if have.SignatureHTML == "" {
					have.SignatureHTML = al.SignatureHTML
				}
				if have.ReplyTo == "" {
					have.ReplyTo = al.ReplyTo
				}
				continue
			}
			// With no display name from the provider, fall back to the
			// account's name, but never to an address: people would see
			// "jacob@x.com <jacob@x.com>".
			name := al.Name
			if name == "" && !strings.Contains(acct.Name, "@") {
				name = acct.Name
			}
			d.Identities = append(d.Identities, mail.Identity{
				ID: store.NewID(), AccountID: accountID, Email: al.Email, Name: name,
				ReplyTo: al.ReplyTo, SignatureHTML: al.SignatureHTML, SignatureOnReplies: true, Default: al.Default, Imported: true,
			})
			added++
		}
		// Make sure the account has exactly one default.
		def := -1
		for i := range d.Identities {
			if d.Identities[i].AccountID == accountID && d.Identities[i].Default {
				if def >= 0 {
					d.Identities[i].Default = false
				} else {
					def = i
				}
			}
		}
		if def < 0 {
			for i := range d.Identities {
				if d.Identities[i].AccountID == accountID {
					d.Identities[i].Default = true
					break
				}
			}
		}
		return nil
	})
	return added, err
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	var in store.Settings
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	err := a.st.Update(func(d *store.Data) error {
		if in.GoogleClientSecret == "saved" {
			in.GoogleClientSecret = d.Settings.GoogleClientSecret
		}
		if in.AnthropicKey == "saved" {
			in.AnthropicKey = d.Settings.AnthropicKey
		}
		if in.OpenAIKey == "saved" {
			in.OpenAIKey = d.Settings.OpenAIKey
		}
		in.AnthropicKey = strings.TrimSpace(in.AnthropicKey)
		in.OpenAIKey = strings.TrimSpace(in.OpenAIKey)
		in.GoogleClientID = strings.TrimSpace(in.GoogleClientID)
		in.GoogleClientSecret = strings.TrimSpace(in.GoogleClientSecret)
		in.MicrosoftClientID = strings.TrimSpace(in.MicrosoftClientID)
		d.Settings = in
		return nil
	})
	return map[string]bool{"ok": err == nil}, err
}

func (a *App) handleConnect(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Provider string `json:"provider"`
		// AccountID reconnects an existing account.
		AccountID string `json:"accountId"`
		// Started, when true, only returns the URL so the window can show it.
		Started bool `json:"started"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if a.demo {
		return nil, fmt.Errorf("demo mode can't connect real accounts; start Project Echo Mike without -demo")
	}
	return a.connect(r.Context(), in.Provider, in.AccountID)
}

func (a *App) handleConnectCancel(w http.ResponseWriter, r *http.Request) (any, error) {
	a.mu.Lock()
	f := a.flow
	a.mu.Unlock()
	if f != nil {
		f.Cancel()
	}
	return map[string]bool{"ok": true}, nil
}

func (a *App) handleAccountUpdate(w http.ResponseWriter, r *http.Request) (any, error) {
	var in store.Account
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	err := a.st.Update(func(d *store.Data) error {
		for i := range d.Accounts {
			if d.Accounts[i].ID == in.ID {
				d.Accounts[i].Name = strings.TrimSpace(in.Name)
				d.Accounts[i].Color = in.Color
				d.Accounts[i].AIEngine = in.AIEngine
				return nil
			}
		}
		return fmt.Errorf("account not found")
	})
	return map[string]bool{"ok": err == nil}, err
}

func (a *App) handleAccountRemove(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	err := a.st.Update(func(d *store.Data) error {
		accts := d.Accounts[:0]
		for _, ac := range d.Accounts {
			if ac.ID != in.ID {
				accts = append(accts, ac)
			}
		}
		d.Accounts = accts
		ids := d.Identities[:0]
		for _, x := range d.Identities {
			if x.AccountID != in.ID {
				ids = append(ids, x)
			} else if d.Settings.DefaultIdentity == x.ID {
				d.Settings.DefaultIdentity = ""
			}
		}
		d.Identities = ids
		return nil
	})
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	delete(a.providers, in.ID)
	a.mu.Unlock()
	return map[string]bool{"ok": true}, a.st.SetSecret(in.ID, "")
}

func (a *App) handleUnread(w http.ResponseWriter, r *http.Request) (any, error) {
	out := map[string]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	for _, ac := range a.st.Get().Accounts {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			p, _, err := a.provider(id)
			if err != nil {
				return
			}
			n, err := p.Unread(ctx)
			if err != nil {
				a.noteErr(id, err)
				return
			}
			mu.Lock()
			out[id] = n
			mu.Unlock()
		}(ac.ID)
	}
	wg.Wait()
	return out, nil
}

// handleInviteRespond accepts, tentatively accepts or declines an invitation
// as the alias it was sent to.
func (a *App) handleInviteRespond(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		AccountID string `json:"accountId"`
		ID        string `json:"id"`
		Status    string `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	status := strings.ToUpper(in.Status)
	if status != "ACCEPTED" && status != "TENTATIVE" && status != "DECLINED" {
		return nil, fmt.Errorf("unknown response %q", in.Status)
	}
	p, _, err := a.provider(in.AccountID)
	if err != nil {
		return nil, err
	}
	m, err := p.Get(r.Context(), in.ID)
	if err != nil {
		return nil, err
	}
	if m.Invite == nil {
		return nil, fmt.Errorf("that message has no invitation")
	}
	id, _ := mail.PickIdentity(a.st.Get().Identities, in.AccountID, m)
	me := mail.Address{Name: displayName(&id), Email: id.Email}
	if me.Email == "" {
		return nil, fmt.Errorf("add a From address for this account first")
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := p.Respond(ctx, in.ID, m.Invite, me, status); err != nil {
		a.noteErr(in.AccountID, err)
		return nil, err
	}
	return map[string]string{"status": status, "as": me.Email}, nil
}

func (a *App) handleUpdateStatus(w http.ResponseWriter, r *http.Request) (any, error) {
	if a.Updater == nil {
		return map[string]any{"supported": false}, nil
	}
	return map[string]any{"supported": true, "info": a.Updater.Status()}, nil
}

func (a *App) handleUpdateCheck(w http.ResponseWriter, r *http.Request) (any, error) {
	if a.Updater == nil {
		return nil, fmt.Errorf("updates aren't available in this build")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	info, err := a.Updater.Check(ctx)
	if err != nil {
		return nil, fmt.Errorf("couldn't check for updates: %w", err)
	}
	return info, nil
}

// handleUpdateInstall downloads the new version, swaps it in and restarts.
func (a *App) handleUpdateInstall(w http.ResponseWriter, r *http.Request) (any, error) {
	if a.Updater == nil {
		return nil, fmt.Errorf("updates aren't available in this build")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	if !a.Updater.Status().Ready {
		if err := a.Updater.Download(ctx, nil); err != nil {
			return nil, err
		}
	}
	if err := a.Updater.Apply(); err != nil {
		return nil, err
	}
	if a.Restart != nil {
		go func() {
			time.Sleep(300 * time.Millisecond)
			a.Restart()
		}()
	}
	return map[string]bool{"ok": true}, nil
}

// WatchUpdates checks for a new version shortly after start and once a day
// after that, and tells the window when one is found.
func (a *App) WatchUpdates(ctx context.Context, found func(update.Info)) {
	if a.Updater == nil {
		return
	}
	delay := 20 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = 24 * time.Hour
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		info, err := a.Updater.Check(cctx)
		cancel()
		if err == nil && info.Available && found != nil {
			found(info)
		}
	}
}

// handleOpen opens a link from a message in the system browser. Links in the
// message frame point here; the 204 leaves the frame where it was.
func (a *App) handleOpen(w http.ResponseWriter, r *http.Request) (any, error) {
	u, err := url.Parse(r.URL.Query().Get("u"))
	if err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto") {
		a.openURL(u.String())
	}
	w.WriteHeader(http.StatusNoContent)
	return nil, nil
}

// displayName is the name shown with an address, left out when it's just the
// address again.
func displayName(id *mail.Identity) string {
	if n := strings.TrimSpace(id.Name); n != "" && !strings.EqualFold(n, id.Email) {
		return n
	}
	return ""
}
