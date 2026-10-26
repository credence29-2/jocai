package app

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

// contacts remembers addresses seen in mail, for suggestions while typing a
// recipient. It's saved to contacts.json next to the settings.
type contacts struct {
	mu     sync.Mutex
	loaded bool
	dirty  bool
	byMail map[string]*contact
}

type contact struct {
	Email string    `json:"email"`
	Name  string    `json:"name,omitempty"`
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

const maxContacts = 3000

func (a *App) loadContacts() {
	if a.book.loaded {
		return
	}
	a.book.loaded = true
	a.book.byMail = map[string]*contact{}
	if b, err := os.ReadFile(a.st.File("contacts.json")); err == nil {
		var list []*contact
		if json.Unmarshal(b, &list) == nil {
			for _, c := range list {
				a.book.byMail[strings.ToLower(c.Email)] = c
			}
		}
	}
}

// learn notes addresses from a message. Sent counts for more than received,
// since people you write to are the ones you'll write to again.
func (a *App) learn(weight int, lists ...[]mail.Address) {
	a.book.mu.Lock()
	defer a.book.mu.Unlock()
	a.loadContacts()
	now := time.Now()
	for _, l := range lists {
		for _, ad := range l {
			e := strings.ToLower(strings.TrimSpace(ad.Email))
			if e == "" || !strings.Contains(e, "@") || strings.Contains(e, "noreply") || strings.Contains(e, "no-reply") {
				continue
			}
			c := a.book.byMail[e]
			if c == nil {
				c = &contact{Email: strings.TrimSpace(ad.Email)}
				a.book.byMail[e] = c
			}
			c.Count += weight
			c.Last = now
			if n := strings.TrimSpace(ad.Name); n != "" && !strings.EqualFold(n, c.Email) {
				c.Name = n
			}
			a.book.dirty = true
		}
	}
	if len(a.book.byMail) > maxContacts {
		// Drop the least used.
		all := a.sorted()
		for _, c := range all[maxContacts-maxContacts/10:] {
			delete(a.book.byMail, strings.ToLower(c.Email))
		}
	}
}

// sorted returns contacts best first. Call with the lock held.
func (a *App) sorted() []*contact {
	all := make([]*contact, 0, len(a.book.byMail))
	for _, c := range a.book.byMail {
		all = append(all, c)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Count != all[j].Count {
			return all[i].Count > all[j].Count
		}
		return all[i].Last.After(all[j].Last)
	})
	return all
}

// saveContacts writes the book if it changed. Called after a list or send,
// off the request path.
func (a *App) saveContacts() {
	a.book.mu.Lock()
	defer a.book.mu.Unlock()
	if !a.book.dirty {
		return
	}
	all := a.sorted()
	if b, err := json.Marshal(all); err == nil {
		os.WriteFile(a.st.File("contacts.json"), b, 0o600)
	}
	a.book.dirty = false
}

// handleContacts suggests addresses for what's been typed so far.
func (a *App) handleContacts(w http.ResponseWriter, r *http.Request) (any, error) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	a.book.mu.Lock()
	defer a.book.mu.Unlock()
	a.loadContacts()
	type hit struct {
		Email string `json:"email"`
		Name  string `json:"name,omitempty"`
		score int
	}
	var hits []hit
	// Identities count too: your own aliases are easy to forget to spell.
	own := map[string]bool{}
	for _, id := range a.st.Get().Identities {
		own[strings.ToLower(id.Email)] = true
		if q == "" || strings.HasPrefix(strings.ToLower(id.Email), q) || wordPrefix(id.Name, q) {
			hits = append(hits, hit{Email: id.Email, Name: id.Name, score: 1})
		}
	}
	for _, c := range a.sorted() {
		if own[strings.ToLower(c.Email)] {
			continue
		}
		e := strings.ToLower(c.Email)
		switch {
		case q == "":
			hits = append(hits, hit{Email: c.Email, Name: c.Name, score: c.Count})
		case strings.HasPrefix(e, q) || wordPrefix(c.Name, q):
			hits = append(hits, hit{Email: c.Email, Name: c.Name, score: c.Count + 1000})
		case strings.Contains(e, q) || strings.Contains(strings.ToLower(c.Name), q):
			hits = append(hits, hit{Email: c.Email, Name: c.Name, score: c.Count})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > 8 {
		hits = hits[:8]
	}
	if hits == nil {
		hits = []hit{}
	}
	return hits, nil
}

// wordPrefix reports whether any word of name starts with q.
func wordPrefix(name, q string) bool {
	if q == "" {
		return false
	}
	for _, w := range strings.Fields(strings.ToLower(name)) {
		if strings.HasPrefix(w, q) {
			return true
		}
	}
	return strings.HasPrefix(strings.ToLower(name), q)
}
