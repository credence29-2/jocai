package app

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

func TestCalendarRoutes(t *testing.T) {
	hs := newHarness(t)
	var cals calendarsOut
	if code := hs.call("GET", "/api/cal/calendars", nil, &cals); code != 200 || len(cals.Calendars) != 2 {
		t.Fatalf("calendars %d %+v", code, cals)
	}
	today := time.Now()
	from := today.AddDate(0, 0, -7).Format("2006-01-02")
	to := today.AddDate(0, 0, 14).Format("2006-01-02")
	var evs eventsOut
	q := url.Values{"from": {from}, "to": {to}}
	if code := hs.call("GET", "/api/cal/events?"+q.Encode(), nil, &evs); code != 200 || len(evs.Events) < 6 {
		t.Fatalf("events %d %d %v", code, len(evs.Events), evs.Errors)
	}
	// All-day events sort first, the rest by start.
	if !evs.Events[0].AllDay {
		t.Fatalf("first event %+v", evs.Events[0])
	}
	var kick *mail.Event
	for i := range evs.Events {
		if strings.HasPrefix(evs.Events[i].Title, "Onboarding kickoff") {
			kick = &evs.Events[i]
		}
	}
	if kick == nil || kick.Mine || kick.MyStatus != "needsAction" || kick.AccountID != "demo-o" {
		t.Fatalf("kickoff %+v", kick)
	}
	var out map[string]string
	hs.call("POST", "/api/cal/event/respond", map[string]any{"accountId": kick.AccountID, "calendarId": kick.CalendarID, "id": kick.ID, "status": "accepted"}, &out)
	if out["status"] != "accepted" {
		t.Fatalf("respond %v", out)
	}

	// Create, edit, delete.
	start := time.Date(today.Year(), today.Month(), today.Day(), 15, 0, 0, 0, time.Local).AddDate(0, 0, 1)
	var ev mail.Event
	in := map[string]any{"accountId": "demo-g", "calendarId": "primary", "title": " Printer vendor call ", "start": start, "end": start.Add(30 * time.Minute), "attendees": []map[string]string{{"email": "derek@posdealer.example", "name": "Derek"}}, "notify": true}
	if code := hs.call("POST", "/api/cal/event", in, &ev); code != 200 || ev.Title != "Printer vendor call" || ev.ID == "" || len(ev.Attendees) != 2 {
		t.Fatalf("create %d %+v", code, ev)
	}
	in["id"], in["title"], in["allDay"], in["end"] = ev.ID, "Vendor day", true, start
	var ev2 mail.Event
	if code := hs.call("POST", "/api/cal/event", in, &ev2); code != 200 || !ev2.AllDay || ev2.Start.Hour() != 0 || ev2.ID != ev.ID {
		t.Fatalf("update %d %+v", code, ev2)
	}
	var errOut map[string]string
	if code := hs.call("POST", "/api/cal/event", map[string]any{"accountId": "demo-g", "title": "x", "start": start, "end": start}, &errOut); code != 500 || !strings.Contains(errOut["error"], "end has to be after") {
		t.Fatalf("bad range: %d %v", code, errOut)
	}
	if code := hs.call("POST", "/api/cal/event/delete", map[string]any{"accountId": "demo-g", "calendarId": "primary", "id": ev.ID}, &out); code != 200 {
		t.Fatal(code)
	}
	hs.call("GET", "/api/cal/events?"+q.Encode(), nil, &evs)
	for _, e := range evs.Events {
		if e.ID == ev.ID {
			t.Fatal("deleted event still listed")
		}
	}
	// Attendees typed in are remembered for suggestions.
	var hits []map[string]any
	hs.call("GET", "/api/contacts?q=derek", nil, &hits)
	if len(hits) == 0 {
		t.Fatal("attendee not learned")
	}
}
