package gmail

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	cm "github.com/credence29-2/jocai/echo-mike/internal/mail"
)

func TestCalendar(t *testing.T) {
	var calls []string
	var lastBody string
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		switch {
		case r.URL.Path == "/gmail/v1/users/me/profile":
			w.Write([]byte(`{"emailAddress":"jacob@gmail.com"}`))
		case r.URL.Path == "/calendar/v3/users/me/calendarList":
			w.Write([]byte(`{"items":[{"id":"jacob@gmail.com","summary":"jacob@gmail.com","primary":true,"accessRole":"owner","backgroundColor":"#9fe1e7"},
				{"id":"holidays","summary":"Holidays","accessRole":"reader"},{"id":"hidden","summary":"x","hidden":true,"accessRole":"owner"}]}`))
		case r.URL.Path == "/calendar/v3/calendars/jacob@gmail.com/events" && r.Method == "GET":
			if r.URL.Query().Get("singleEvents") != "true" || r.URL.Query().Get("timeMin") == "" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"items":[
				{"id":"e1","status":"confirmed","summary":"Standup","start":{"dateTime":"2026-10-27T09:00:00-04:00"},"end":{"dateTime":"2026-10-27T09:15:00-04:00"},"organizer":{"email":"jacob@gmail.com","self":true},"recurringEventId":"r1","htmlLink":"https://cal/e1",
				  "attendees":[{"email":"jacob@gmail.com","self":true,"responseStatus":"accepted","organizer":true},{"email":"kevin@x.com","displayName":"Kevin","responseStatus":"needsAction"}]},
				{"id":"e2","status":"confirmed","summary":"Kickoff","start":{"dateTime":"2026-10-29T14:00:00-04:00"},"end":{"dateTime":"2026-10-29T15:00:00-04:00"},"organizer":{"email":"priya@x.com"},
				  "attendees":[{"email":"priya@x.com","organizer":true,"responseStatus":"accepted"},{"email":"jacob@gmail.com","self":true,"responseStatus":"needsAction"}]},
				{"id":"gone","status":"cancelled"},
				{"id":"e3","status":"confirmed","start":{"date":"2026-10-30"},"end":{"date":"2026-10-31"}}]}`))
		case r.URL.Path == "/calendar/v3/calendars/holidays/events" && r.Method == "GET":
			w.Write([]byte(`{"items":[{"id":"h1","status":"confirmed","summary":"Halloween","start":{"date":"2026-10-31"},"end":{"date":"2026-11-01"}}]}`))
		case r.URL.Path == "/calendar/v3/calendars/primary/events" && r.Method == "POST":
			w.Write([]byte(`{"id":"new1","summary":"Lunch","start":{"dateTime":"2026-10-28T12:00:00-04:00"},"end":{"dateTime":"2026-10-28T13:00:00-04:00"},"organizer":{"email":"jacob@gmail.com","self":true}}`))
		case r.URL.Path == "/calendar/v3/calendars/c1/events/e2" && r.Method == "GET":
			w.Write([]byte(`{"id":"e2","attendees":[{"email":"priya@x.com","responseStatus":"accepted"},{"email":"jacob@gmail.com","self":true,"responseStatus":"needsAction"}]}`))
		case r.URL.Path == "/calendar/v3/calendars/c1/events/e2" && r.Method == "PATCH":
			w.Write([]byte(`{"id":"e2"}`))
		case r.URL.Path == "/calendar/v3/calendars/c1/events/e1" && r.Method == "DELETE":
			w.WriteHeader(204)
		case r.URL.Path == "/calendar/v3/calendars/c1/events/old" && r.Method == "DELETE":
			w.WriteHeader(410)
			w.Write([]byte(`{"error":{"message":"Resource has been deleted"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	ctx := context.Background()
	c.Email(ctx)
	cals, err := c.Calendars(ctx)
	if err != nil || len(cals) != 2 || !cals[0].Primary || cals[0].ReadOnly || !cals[1].ReadOnly || cals[0].Color != "#9fe1e7" {
		t.Fatalf("calendars: %+v %v", cals, err)
	}
	evs, err := c.Events(ctx, time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	if err != nil || len(evs) != 4 {
		t.Fatalf("events: %d %v", len(evs), err)
	}
	byID := map[string]cm.Event{}
	for _, e := range evs {
		byID[e.ID] = e
	}
	e1, e2, e3, h1 := byID["e1"], byID["e2"], byID["e3"], byID["h1"]
	if !e1.Mine || !e1.Recurring || e1.MyStatus != "accepted" || len(e1.Attendees) != 2 || e1.Attendees[1].Name != "Kevin" || e1.Start.UTC().Hour() != 13 || e1.Link != "https://cal/e1" {
		t.Fatalf("e1 %+v", e1)
	}
	if e2.Mine || e2.MyStatus != "needsAction" || !e2.Attendees[0].Organizer {
		t.Fatalf("e2 %+v", e2)
	}
	if !e3.AllDay || e3.Title != "(no title)" || e3.Start.Day() != 30 {
		t.Fatalf("e3 %+v", e3)
	}
	if h1.Mine || !h1.AllDay {
		t.Fatalf("holiday should be read only: %+v", h1)
	}

	// Create goes to the primary calendar with attendees and notifications.
	in := cm.EventInput{Title: "Lunch", Start: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 28, 13, 0, 0, 0, time.UTC), Attendees: []cm.Address{{Name: "Kevin", Email: "kevin@x.com"}}, Notify: true}
	ev, err := c.CreateEvent(ctx, in)
	if err != nil || ev.ID != "new1" || !ev.Mine {
		t.Fatalf("create: %+v %v", ev, err)
	}
	var body map[string]any
	json.Unmarshal([]byte(lastBody), &body)
	if body["summary"] != "Lunch" || len(body["attendees"].([]any)) != 1 || !strings.Contains(calls[len(calls)-1], "sendUpdates=all") {
		t.Fatalf("create body %s / %s", lastBody, calls[len(calls)-1])
	}
	// All-day events use dates, with Google's exclusive end.
	in.AllDay, in.End = true, in.Start
	c.CreateEvent(ctx, in)
	json.Unmarshal([]byte(lastBody), &body)
	if body["start"].(map[string]any)["date"] != "2026-10-28" || body["end"].(map[string]any)["date"] != "2026-10-29" {
		t.Fatalf("all-day body %s", lastBody)
	}

	// Responding patches only my attendee line.
	if err := c.RespondEvent(ctx, "c1", "e2", "accepted"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lastBody, `"email":"jacob@gmail.com","responseStatus":"accepted"`) || !strings.Contains(lastBody, `"priya@x.com","responseStatus":"accepted"`) {
		t.Fatalf("respond body %s", lastBody)
	}
	if err := c.DeleteEvent(ctx, "c1", "e1", false); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteEvent(ctx, "c1", "old", false); err != nil {
		t.Fatalf("deleting a gone event should be fine: %v", err)
	}
}

func TestCalendarScopeMissing(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"error":{"code":403,"message":"Request had insufficient authentication scopes.","status":"PERMISSION_DENIED"}}`))
	})
	_, err := c.Calendars(context.Background())
	if !errors.Is(err, cm.ErrCalendarScope) {
		t.Fatalf("got %v", err)
	}
}

func TestLabels(t *testing.T) {
	var lastBody string
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		switch {
		case r.URL.Path == "/gmail/v1/users/me/labels":
			w.Write([]byte(`{"labels":[{"id":"INBOX","name":"INBOX","type":"system"},{"id":"Label_7","name":"Clients","type":"user","color":{"backgroundColor":"#16a765"}},{"id":"Label_2","name":"Billing","type":"user"}]}`))
		case r.URL.Path == "/gmail/v1/users/me/messages" && r.URL.Query().Get("labelIds") == "Label_7":
			w.Write([]byte(`{"messages":[{"id":"m1"}]}`))
		case r.URL.Path == "/gmail/v1/users/me/messages/m1":
			w.Write([]byte(`{"id":"m1","threadId":"t","labelIds":["INBOX","Label_7","CATEGORY_PERSONAL"],"internalDate":"1700000000000","payload":{"headers":[{"name":"Subject","value":"x"}]}}`))
		case r.URL.Path == "/gmail/v1/users/me/messages/m1/modify":
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	ctx := context.Background()
	labels, err := c.Labels(ctx)
	if err != nil || len(labels) != 2 || labels[0].Name != "Billing" || labels[1].Color != "#16a765" {
		t.Fatalf("labels %+v %v", labels, err)
	}
	page, err := c.List(ctx, cm.LabelFolder("Label_7"), "", "", 30)
	if err != nil || len(page.Messages) != 1 || strings.Join(page.Messages[0].Labels, ",") != "Label_7" {
		t.Fatalf("list %+v %v", page, err)
	}
	if err := c.SetLabel(ctx, "m1", "Label_2", true); err != nil || lastBody != `{"addLabelIds":["Label_2"]}` {
		t.Fatalf("add %v %s", err, lastBody)
	}
	c.SetLabel(ctx, "m1", "Label_7", false)
	if lastBody != `{"removeLabelIds":["Label_7"]}` {
		t.Fatalf("remove %s", lastBody)
	}
	page, _ = c.List(ctx, cm.LabelFolder("Label_7"), "", "", 30)
	if strings.Join(page.Messages[0].Labels, ",") != "Label_2" {
		t.Fatalf("cache not updated: %v", page.Messages[0].Labels)
	}
}
