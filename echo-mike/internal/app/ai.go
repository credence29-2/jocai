package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/credence29-2/jocai/echo-mike/internal/ai"
	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

// aiClient builds a client from the saved settings, with the account's
// own engine choice applied when it has one.
func (a *App) aiClient(accountID string) *ai.Client {
	d := a.st.Get()
	cfg := d.Settings.Config
	for _, ac := range d.Accounts {
		if ac.ID == accountID {
			cfg = cfg.ForAccount(ac.AIEngine)
		}
	}
	return ai.New(cfg)
}

// handleAITest tries the engine described in the request, so Settings can
// check a key or model before saving it.
func (a *App) handleAITest(w http.ResponseWriter, r *http.Request) (any, error) {
	var in ai.Config
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.AnthropicKey == "saved" {
		in.AnthropicKey = a.st.Get().Settings.AnthropicKey
	}
	if in.OpenAIKey == "saved" {
		in.OpenAIKey = a.st.Get().Settings.OpenAIKey
	}
	line, err := ai.New(in).Test(r.Context())
	if err != nil {
		return nil, err
	}
	return map[string]string{"message": line}, nil
}

type aiRunIn struct {
	// Task is summarize, draft or rewrite.
	Task string `json:"task"`
	// AccountID picks the engine (accounts can have their own). With ID it
	// names the message for summarize and draft; summarize takes the whole
	// conversation when ThreadID is set.
	AccountID string `json:"accountId"`
	ID        string `json:"id"`
	ThreadID  string `json:"threadId"`
	// Note is what the reply should say; Name signs it.
	Note string `json:"note"`
	Name string `json:"name"`
	// Mode and Text are for rewrite.
	Mode string `json:"mode"`
	Text string `json:"text"`
}

// handleAIRun streams the model's answer as JSON lines: {"t":"..."} for each
// piece of text, then {"done":true} or {"error":"..."}.
func (a *App) handleAIRun(w http.ResponseWriter, r *http.Request) (any, error) {
	var in aiRunIn
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	var system, prompt string
	switch in.Task {
	case "summarize":
		text, err := a.mailText(r.Context(), in.AccountID, in.ID, in.ThreadID)
		if err != nil {
			return nil, err
		}
		system, prompt = ai.Summarize(text)
	case "draft":
		if strings.TrimSpace(in.Note) == "" {
			return nil, fmt.Errorf("say what the reply should cover first")
		}
		if in.ID == "" && in.ThreadID == "" {
			system, prompt = ai.DraftNew(in.Note, in.Name)
			break
		}
		text, err := a.mailText(r.Context(), in.AccountID, in.ID, in.ThreadID)
		if err != nil {
			return nil, err
		}
		system, prompt = ai.DraftReply(in.Note, text, in.Name)
	case "rewrite":
		if strings.TrimSpace(in.Text) == "" {
			return nil, fmt.Errorf("write something first")
		}
		var ok bool
		if system, prompt, ok = ai.Rewrite(in.Mode, in.Text); !ok {
			return nil, fmt.Errorf("unknown rewrite %q", in.Mode)
		}
	case "search":
		if strings.TrimSpace(in.Text) == "" {
			return nil, fmt.Errorf("say what you're looking for first")
		}
		var names []string
		for _, ac := range a.st.Get().Accounts {
			if p, _, err := a.provider(ac.ID); err == nil {
				if lb, ok := p.(mail.Labeler); ok {
					if labels, err := lb.Labels(r.Context()); err == nil {
						for _, l := range labels {
							names = append(names, l.Name)
						}
					}
				}
			}
		}
		system, prompt = ai.Search(in.Text, time.Now(), names)
	default:
		return nil, fmt.Errorf("unknown task %q", in.Task)
	}
	c := a.aiClient(in.AccountID)
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	send := func(v any) {
		enc.Encode(v)
		if flusher != nil {
			flusher.Flush()
		}
	}
	_, err := c.Stream(r.Context(), system, prompt, func(s string) { send(map[string]string{"t": s}) })
	if err != nil {
		send(map[string]string{"error": err.Error()})
	} else {
		send(map[string]bool{"done": true})
	}
	return nil, nil
}

// mailText renders a message, or its whole conversation, as plain text for
// the model: headers first, then the body with quoted replies left in.
func (a *App) mailText(ctx context.Context, accountID, id, threadID string) (string, error) {
	if accountID == "" || (id == "" && threadID == "") {
		return "", fmt.Errorf("open a message first")
	}
	p, _, err := a.provider(accountID)
	if err != nil {
		return "", err
	}
	var msgs []*mail.Message
	if threadID != "" {
		msgs, err = p.Thread(ctx, threadID)
	} else {
		var m *mail.Message
		m, err = p.Get(ctx, id)
		msgs = []*mail.Message{m}
	}
	if err != nil {
		a.noteErr(accountID, err)
		return "", err
	}
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteString("\n\n-----\n\n")
		}
		fmt.Fprintf(&b, "From: %s\nTo: %s\n", addrLine([]mail.Address{m.From}), addrLine(m.To))
		if len(m.Cc) > 0 {
			fmt.Fprintf(&b, "Cc: %s\n", addrLine(m.Cc))
		}
		fmt.Fprintf(&b, "Date: %s\nSubject: %s\n\n", m.Date.Local().Format("Mon Jan 2, 2006 3:04 PM"), m.Subject)
		body := m.Text
		if strings.TrimSpace(body) == "" {
			body = mail.HTMLToText(m.HTML)
		}
		if strings.TrimSpace(body) == "" {
			body = m.Snippet
		}
		b.WriteString(strings.TrimSpace(body))
		for _, f := range m.Attachments {
			if !f.Inline {
				fmt.Fprintf(&b, "\n[attachment: %s]", f.Name)
			}
		}
	}
	return b.String(), nil
}

func addrLine(l []mail.Address) string {
	var parts []string
	for _, a := range l {
		if a.Name != "" {
			parts = append(parts, a.Name+" <"+a.Email+">")
		} else {
			parts = append(parts, a.Email)
		}
	}
	return strings.Join(parts, ", ")
}
