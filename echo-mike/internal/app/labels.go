package app

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

type labelEntry struct {
	mail.Label
	AccountID string `json:"accountId"`
}

type labelsOut struct {
	Labels []labelEntry      `json:"labels"`
	Errors map[string]string `json:"errors"`
}

// handleLabels lists every account's labels (Gmail) or own folders (Outlook).
func (a *App) handleLabels(w http.ResponseWriter, r *http.Request) (any, error) {
	out := labelsOut{Labels: []labelEntry{}, Errors: map[string]string{}}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, acct := range a.st.Get().Accounts {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			p, _, err := a.provider(id)
			var labels []mail.Label
			if err == nil {
				if lb, ok := p.(mail.Labeler); ok {
					labels, err = lb.Labels(r.Context())
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				a.noteErr(id, err)
				out.Errors[id] = err.Error()
				return
			}
			for _, l := range labels {
				out.Labels = append(out.Labels, labelEntry{Label: l, AccountID: id})
			}
		}(acct.ID)
	}
	wg.Wait()
	return out, nil
}

// handleLabel adds or removes one label on messages, or on whole
// conversations.
func (a *App) handleLabel(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		AccountID string   `json:"accountId"`
		IDs       []string `json:"ids"`
		Threads   []string `json:"threads"`
		LabelID   string   `json:"labelId"`
		On        bool     `json:"on"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.LabelID == "" {
		return nil, fmt.Errorf("no label given")
	}
	p, _, err := a.provider(in.AccountID)
	if err != nil {
		return nil, err
	}
	lb, ok := p.(mail.Labeler)
	if !ok {
		return nil, fmt.Errorf("this account has no labels")
	}
	for _, t := range in.Threads {
		msgs, err := p.Thread(r.Context(), t)
		if err != nil {
			a.noteErr(in.AccountID, err)
			return nil, err
		}
		for _, m := range msgs {
			if !m.Hidden {
				in.IDs = append(in.IDs, m.ID)
			}
		}
	}
	for _, id := range in.IDs {
		if err := lb.SetLabel(r.Context(), id, in.LabelID, in.On); err != nil {
			a.noteErr(in.AccountID, err)
			return nil, err
		}
	}
	return map[string]bool{"ok": true}, nil
}
