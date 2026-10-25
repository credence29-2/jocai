package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

// watcher remembers which inbox messages have been seen, so only mail that
// arrives while the app is open raises a notification.
type watcher struct {
	mu     sync.Mutex
	seen   map[string]map[string]bool
	seeded map[string]bool
}

// watchMail checks every account's inbox for new mail until ctx ends.
func (a *app) watchMail(ctx context.Context, every time.Duration) {
	for {
		a.checkMail(ctx)
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
func (a *app) checkMail(ctx context.Context) {
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
		if a.watch.seen == nil {
			a.watch.seen, a.watch.seeded = map[string]map[string]bool{}, map[string]bool{}
		}
		seen := a.watch.seen[ac.ID]
		next := map[string]bool{}
		for _, m := range page.Messages {
			next[m.ID] = true
			if a.watch.seeded[ac.ID] && !seen[m.ID] && m.Unread {
				m.AccountID = ac.ID
				fresh = append(fresh, m)
			}
		}
		// Only this page is kept, so the set stays small.
		a.watch.seen[ac.ID] = next
		a.watch.seeded[ac.ID] = true
		a.watch.mu.Unlock()
	}
	if len(fresh) == 0 {
		return
	}
	if a.onNewMail != nil {
		a.onNewMail()
	}
	if a.notify == nil || a.st.Get().Settings.MuteNotifications {
		return
	}
	a.notify(notification(fresh, names, len(d.Accounts) > 1))
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
