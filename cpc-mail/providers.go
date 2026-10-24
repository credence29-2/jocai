package main

import (
	"context"
	"fmt"

	"github.com/credence29-2/jocai/cpc-mail/internal/mail"
	"github.com/credence29-2/jocai/cpc-mail/internal/store"
)

// openProvider connects to a saved account using its stored sign-in.
func (a *app) openProvider(acct store.Account) (mail.Provider, error) {
	return nil, fmt.Errorf("%s accounts aren't supported yet", acct.Provider)
}

// connect signs a new account in through the browser, or refreshes the
// sign-in of an existing one.
func (a *app) connect(ctx context.Context, provider, accountID string) (any, error) {
	return nil, fmt.Errorf("not yet")
}
