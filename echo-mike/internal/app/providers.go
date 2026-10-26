package app

import (
	"context"
	"fmt"

	"github.com/credence29-2/jocai/echo-mike/internal/gmail"
	"github.com/credence29-2/jocai/echo-mike/internal/mail"
	"github.com/credence29-2/jocai/echo-mike/internal/oauth"
	"github.com/credence29-2/jocai/echo-mike/internal/outlook"
	"github.com/credence29-2/jocai/echo-mike/internal/store"
)

// signIn returns the OAuth setup for a provider from the saved client IDs.
func (a *App) signIn(provider string) (*oauth.Provider, error) {
	s := a.st.Get().Settings
	switch provider {
	case "gmail":
		if s.GoogleClientID == "" || s.GoogleClientSecret == "" {
			return nil, fmt.Errorf("fill in the Google client ID and secret in Settings, Sign-in setup")
		}
		return oauth.Google(s.GoogleClientID, s.GoogleClientSecret), nil
	case "outlook":
		if s.MicrosoftClientID == "" {
			return nil, fmt.Errorf("fill in the Microsoft client ID in Settings, Sign-in setup")
		}
		return oauth.Microsoft(s.MicrosoftClientID), nil
	}
	return nil, fmt.Errorf("unknown provider %q", provider)
}

func (a *App) build(provider, email string, src *oauth.Source) mail.Provider {
	if provider == "gmail" {
		return gmail.New(src)
	}
	return outlook.New(src, email)
}

// openProvider connects to a saved account using its stored sign-in.
func (a *App) openProvider(acct store.Account) (mail.Provider, error) {
	refresh := a.st.Secret(acct.ID)
	if refresh == "" {
		return nil, mail.ErrAuth
	}
	p, err := a.signIn(acct.Provider)
	if err != nil {
		return nil, err
	}
	src := oauth.NewSource(p, refresh, nil, func(r string) { a.st.SetSecret(acct.ID, r) })
	return a.build(acct.Provider, acct.Email, src), nil
}

type connectOut struct {
	AccountID string `json:"accountId"`
	Email     string `json:"email"`
	Aliases   int    `json:"aliases"`
}

// connect signs an account in through the browser. With accountID it
// refreshes an existing account's sign-in instead of adding one.
func (a *App) connect(ctx context.Context, provider, accountID string) (any, error) {
	p, err := a.signIn(provider)
	if err != nil {
		return nil, err
	}
	var existing *store.Account
	for _, ac := range a.st.Get().Accounts {
		if ac.ID == accountID {
			ac := ac
			existing = &ac
		}
	}
	hint := ""
	if existing != nil {
		hint = existing.Email
	}
	flow, err := oauth.Start(p, hint)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.flow != nil {
		a.flow.Cancel()
	}
	a.flow = flow
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.flow == flow {
			a.flow = nil
		}
		a.mu.Unlock()
	}()
	if err := a.openURL(flow.URL); err != nil {
		flow.Cancel()
		return nil, fmt.Errorf("couldn't open your browser: %w", err)
	}
	tok, err := flow.Wait(ctx)
	if err != nil {
		if ctx.Err() != nil || err == context.Canceled {
			return nil, fmt.Errorf("sign-in cancelled")
		}
		return nil, err
	}

	var saveTo string
	src := oauth.NewSource(p, tok.RefreshToken, tok, func(r string) {
		if saveTo != "" {
			a.st.SetSecret(saveTo, r)
		}
	})
	var email, name string
	switch provider {
	case "gmail":
		email, err = gmail.New(src).Email(ctx)
	default:
		email, name, err = outlook.New(src, "").Me(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("signed in, but couldn't read the mailbox: %w", err)
	}
	if tok.RefreshToken == "" {
		return nil, fmt.Errorf("%s didn't allow offline access; try again and approve every permission", p.Name)
	}

	// Match an account that's already here, by ID or by address.
	if existing == nil {
		for _, ac := range a.st.Get().Accounts {
			if ac.Provider == provider && mail.SameEmail(ac.Email, email) {
				ac := ac
				existing = &ac
			}
		}
	} else if !mail.SameEmail(existing.Email, email) {
		return nil, fmt.Errorf("you signed in as %s, but this account is %s", email, existing.Email)
	}

	id := store.NewID()
	if existing != nil {
		id = existing.ID
	}
	saveTo = id
	if err := a.st.SetSecret(id, tok.RefreshToken); err != nil {
		return nil, err
	}
	err = a.st.Update(func(d *store.Data) error {
		for i := range d.Accounts {
			if d.Accounts[i].ID == id {
				d.Accounts[i].NeedsReconnect = false
				return nil
			}
		}
		if name == "" {
			name = email
		}
		d.Accounts = append(d.Accounts, store.Account{
			ID: id, Provider: provider, Email: email, Name: name,
			Color: palette[len(d.Accounts)%len(palette)],
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.providers[id] = a.build(provider, email, src)
	a.mu.Unlock()

	if _, err := a.importAliases(ctx, id); err != nil {
		return nil, fmt.Errorf("connected %s, but couldn't load its send-as addresses: %w", email, err)
	}
	n := 0
	for _, x := range a.st.Get().Identities {
		if x.AccountID == id {
			n++
		}
	}
	return connectOut{AccountID: id, Email: email, Aliases: n}, nil
}

var palette = []string{"#d93025", "#0a64d6", "#188038", "#e37400", "#9334e6", "#00897b", "#c2185b", "#5f6368"}
