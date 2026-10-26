package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

// watcher remembers which inbox messages have been seen, so only new mail
// raises a notification. It's saved to seen.json so the Android background
// check and the next start of the app pick up where the last check left off.
type watcher struct {
	mu     sync.Mutex
	loaded bool
	Seen   map[string]map[string]bool `json:"seen"`
	Seeded map[string]bool            `json:"seeded"`
}

func (a *App) loadSeen() {
	if a.watch.loaded {
		return
	}
	a.watch.loaded = true
	if b, err := os.ReadFile(a.st.File("seen.json")); err == nil {
		json.Unmarshal(b, &a.watch)
	}
	if a.watch.Seen == nil || a.watch.Seeded == nil {
		a.watch.Seen, a.watch.Seeded = map[string]map[string]bool{}, map[string]bool{}
	}
}

func (a *App) saveSeen() {
	if b, err := json.Marshal(&a.watch); err == nil {
		os.WriteFile(a.st.File("seen.json"), b, 0o600)
	}
}

// watchMail checks every account's inbox for new mail until ctx ends.
func (a *App) WatchMail(ctx context.Context, every time.Duration) {
	for {
		a.CheckMail(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// checkMail looks at the newest inbox messages in each account and notifies
// about unread ones it hasn't seen before. The first look at an account only
// remembers what's there.
func (a *App) CheckMail(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	d := a.st.Get()
	var fresh []mail.Summary
	names := map[string]string{}
	for _, ac := range d.Accounts {
		if ac.NeedsReconnect {
			continue
		}
		names[ac.ID] = ac.Name
		p, _, err := a.provider(ac.ID)
		if err != nil {
			continue
		}
		page, err := p.List(ctx, mail.Inbox, "", "", 15)
		if err != nil {
			a.noteErr(ac.ID, err)
			continue
		}
		a.watch.mu.Lock()
		a.loadSeen()
		seen := a.watch.Seen[ac.ID]
		next := map[string]bool{}
		for _, m := range page.Messages {
			next[m.ID] = true
			if a.watch.Seeded[ac.ID] && !seen[m.ID] && m.Unread {
				m.AccountID = ac.ID
				fresh = append(fresh, m)
			}
		}
		// Only this page is kept, so the set stays small.
		a.watch.Seen[ac.ID] = next
		a.watch.Seeded[ac.ID] = true
		a.saveSeen()
		a.watch.mu.Unlock()
	}
	if len(fresh) == 0 {
		return
	}
	if a.OnNewMail != nil {
		a.OnNewMail()
	}
	if a.Notify == nil || a.st.Get().Settings.MuteNotifications {
		return
	}
	a.Notify(notification(fresh, names, len(d.Accounts) > 1))
}

// notification words the alert for one or several new messages.
func notification(fresh []mail.Summary, names map[string]string, multi bool) (string, string) {
	who := func(m mail.Summary) string {
		if m.From.Name != "" {
			return m.From.Name
		}
		return m.From.Email
	}
	if len(fresh) == 1 {
		m := fresh[0]
		body := m.Subject
		if body == "" {
			body = "(no subject)"
		}
		if multi && names[m.AccountID] != "" {
			body += "\n" + names[m.AccountID]
		}
		return who(m), body
	}
	var senders []string
	have := map[string]bool{}
	for _, m := range fresh {
		if w := who(m); !have[w] {
			have[w] = true
			senders = append(senders, w)
		}
	}
	body := "From " + strings.Join(senders, ", ")
	if len(senders) > 3 {
		body = fmt.Sprintf("From %s and %d others", strings.Join(senders[:3], ", "), len(senders)-3)
	}
	return fmt.Sprintf("%d new emails", len(fresh)), body
}
