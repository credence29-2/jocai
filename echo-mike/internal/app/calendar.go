package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
	"github.com/credence29-2/jocai/echo-mike/internal/store"
)

// calendar returns the account's calendar side, or an error the page can
// show (reconnect needed, or a provider without calendars).
func (a *App) calendar(accountID string) (mail.CalendarProvider, store.Account, error) {
	p, acct, err := a.provider(accountID)
	if err != nil {
		return nil, acct, err
	}
	cp, ok := p.(mail.CalendarProvider)
	if !ok {
		return nil, acct, fmt.Errorf("this account has no calendar")
	}
	return cp, acct, nil
}

type calendarsOut struct {
	Calendars []calEntry        `json:"calendars"`
	Errors    map[string]string `json:"errors"`
}

type calEntry struct {
	mail.Calendar
	AccountID string `json:"accountId"`
}

// handleCalendars lists every account's calendars.
func (a *App) handleCalendars(w http.ResponseWriter, r *http.Request) (any, error) {
	out := calendarsOut{Calendars: []calEntry{}, Errors: map[string]string{}}
	a.eachCalendar(r.Context(), func(acct store.Account, cp mail.CalendarProvider) error {
		cals, err := cp.Calendars(r.Context())
		if err != nil {
			return err
		}
		for _, c := range cals {
			out.Calendars = append(out.Calendars, calEntry{Calendar: c, AccountID: acct.ID})
		}
		return nil
	}, out.Errors)
	return out, nil
}

type eventsOut struct {
	Events []mail.Event      `json:"events"`
	Errors map[string]string `json:"errors"`
}

// handleEvents lists events across accounts between from and to (RFC 3339
// or YYYY-MM-DD, local).
func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) (any, error) {
	from, err1 := parseWhen(r.URL.Query().Get("from"))
	to, err2 := parseWhen(r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil || !to.After(from) {
		return nil, fmt.Errorf("from and to are needed, as dates")
	}
	if to.Sub(from) > 100*24*time.Hour {
		return nil, fmt.Errorf("that's more than 100 days at once")
	}
	out := eventsOut{Events: []mail.Event{}, Errors: map[string]string{}}
	a.eachCalendar(r.Context(), func(acct store.Account, cp mail.CalendarProvider) error {
		evs, err := cp.Events(r.Context(), from, to)
		if err != nil {
			return err
		}
		for _, e := range evs {
			e.AccountID = acct.ID
			out.Events = append(out.Events, e)
		}
		return nil
	}, out.Errors)
	sort.SliceStable(out.Events, func(i, j int) bool {
		if out.Events[i].AllDay != out.Events[j].AllDay {
			return out.Events[i].AllDay
		}
		return out.Events[i].Start.Before(out.Events[j].Start)
	})
	return out, nil
}

// eachCalendar runs fn for every account with a calendar, in parallel, and
// records failures per account. Results are appended under a lock inside
// fn's caller, so fn must only touch its own account's data.
func (a *App) eachCalendar(ctx context.Context, fn func(store.Account, mail.CalendarProvider) error, errs map[string]string) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for _, acct := range a.st.Get().Accounts {
		wg.Add(1)
		go func(acct store.Account) {
			defer wg.Done()
			cp, _, err := a.calendar(acct.ID)
			if err == nil {
				mu.Lock()
				err = fn(acct, cp)
				mu.Unlock()
			}
			if err != nil {
				a.noteErr(acct.ID, err)
				mu.Lock()
				errs[acct.ID] = calErr(err)
				mu.Unlock()
			}
		}(acct)
	}
	wg.Wait()
}

// calErr turns an error into something the page can act on: the account
// needs reconnecting for the calendar scope, the Calendar API is still off
// in Google Cloud, or a plain message.
func calErr(err error) string {
	if errors.Is(err, mail.ErrCalendarScope) {
		return "needs-reconnect"
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	if strings.Contains(low, "has not been used in project") || strings.Contains(low, "it is disabled") || strings.Contains(low, "accessnotconfigured") || strings.Contains(low, "api not enabled") {
		return "needs-api"
	}
	if strings.Contains(low, "insufficient") && strings.Contains(low, "scope") || strings.Contains(low, "insufficient permission") {
		return "needs-reconnect"
	}
	return msg
}

func parseWhen(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02", s, time.Local)
}

type eventIn struct {
	AccountID string `json:"accountId"`
	// ID is set to update an existing event.
	ID string `json:"id"`
	mail.EventInput
}

// handleEventSave creates or updates an event.
func (a *App) handleEventSave(w http.ResponseWriter, r *http.Request) (any, error) {
	var in eventIn
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return nil, fmt.Errorf("give the event a title")
	}
	if in.Start.IsZero() || in.End.IsZero() {
		return nil, fmt.Errorf("pick a start and end")
	}
	if in.AllDay {
		in.Start = dayOf(in.Start)
		in.End = dayOf(in.End)
		if in.End.Before(in.Start) {
			in.End = in.Start
		}
	} else if !in.End.After(in.Start) {
		return nil, fmt.Errorf("the end has to be after the start")
	}
	for _, at := range in.Attendees {
		if _, err := mail.ParseAddresses(at.Email); err != nil {
			return nil, fmt.Errorf("attendee: %w", err)
		}
	}
	cp, _, err := a.calendar(in.AccountID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	var ev *mail.Event
	if in.ID != "" {
		ev, err = cp.UpdateEvent(ctx, in.CalendarID, in.ID, in.EventInput)
	} else {
		ev, err = cp.CreateEvent(ctx, in.EventInput)
	}
	if err != nil {
		a.noteErr(in.AccountID, err)
		if errors.Is(err, mail.ErrCalendarScope) {
			return nil, fmt.Errorf("reconnect this account under Settings > Accounts to allow calendar access")
		}
		return nil, err
	}
	ev.AccountID = in.AccountID
	a.learn(2, in.Attendees)
	a.saveContacts()
	return ev, nil
}

func dayOf(t time.Time) time.Time {
	t = t.Local()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

func (a *App) handleEventDelete(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		AccountID  string `json:"accountId"`
		CalendarID string `json:"calendarId"`
		ID         string `json:"id"`
		Notify     bool   `json:"notify"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	cp, _, err := a.calendar(in.AccountID)
	if err != nil {
		return nil, err
	}
	if err := cp.DeleteEvent(r.Context(), in.CalendarID, in.ID, in.Notify); err != nil {
		a.noteErr(in.AccountID, err)
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

func (a *App) handleEventRespond(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		AccountID  string `json:"accountId"`
		CalendarID string `json:"calendarId"`
		ID         string `json:"id"`
		Status     string `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	status := strings.ToUpper(in.Status)
	if status != "ACCEPTED" && status != "TENTATIVE" && status != "DECLINED" {
		return nil, fmt.Errorf("unknown response %q", in.Status)
	}
	cp, _, err := a.calendar(in.AccountID)
	if err != nil {
		return nil, err
	}
	if err := cp.RespondEvent(r.Context(), in.CalendarID, in.ID, status); err != nil {
		a.noteErr(in.AccountID, err)
		return nil, err
	}
	return map[string]string{"status": strings.ToLower(status)}, nil
}
