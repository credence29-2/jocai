package main

import (
	"context"
	"testing"

	"github.com/credence29-2/jocai/echo-mike/internal/demo"
	"github.com/credence29-2/jocai/echo-mike/internal/mail"
	"github.com/credence29-2/jocai/echo-mike/internal/store"
)

func TestNewMailNotifications(t *testing.T) {
	hs := newHarness(t)
	var got [][2]string
	refreshed := 0
	hs.a.notify = func(title, body string) { got = append(got, [2]string{title, body}) }
	hs.a.onNewMail = func() { refreshed++ }
	ctx := context.Background()

	// The first check only learns what's already there.
	hs.a.checkMail(ctx)
	if len(got) != 0 || refreshed != 0 {
		t.Fatalf("notified about existing mail: %v", got)
	}

	box := hs.a.providers["demo-g"].(*demo.Box)
	box.Deliver(mail.Address{Name: "Maria Lopez", Email: "maria@tacoshack.example"}, "Printer is back", "<p>Thanks!</p>")
	hs.a.checkMail(ctx)
	if len(got) != 1 || got[0][0] != "Maria Lopez" || got[0][1] != "Printer is back\nPersonal Gmail" || refreshed != 1 {
		t.Fatalf("got %v refreshed %d", got, refreshed)
	}

	// Nothing new, nothing shown.
	hs.a.checkMail(ctx)
	if len(got) != 1 {
		t.Fatalf("repeated: %v", got)
	}

	// Muted: the window still refreshes but no notification.
	hs.a.st.Update(func(d *store.Data) error { d.Settings.MuteNotifications = true; return nil })
	box.Deliver(mail.Address{Email: "a@x.com"}, "One", "")
	box.Deliver(mail.Address{Email: "b@x.com"}, "Two", "")
	hs.a.checkMail(ctx)
	if len(got) != 1 || refreshed != 2 {
		t.Fatalf("muted but got %v", got)
	}

	hs.a.st.Update(func(d *store.Data) error { d.Settings.MuteNotifications = false; return nil })
	box.Deliver(mail.Address{Email: "c@x.com"}, "Three", "")
	box.Deliver(mail.Address{Email: "d@x.com"}, "Four", "")
	hs.a.checkMail(ctx)
	if len(got) != 2 || got[1][0] != "2 new emails" {
		t.Fatalf("batch: %v", got)
	}
}
