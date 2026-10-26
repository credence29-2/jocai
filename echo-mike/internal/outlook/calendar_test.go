package outlook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	cm "github.com/credence29-2/jocai/echo-mike/internal/mail"
)

func TestCalendar(t *testing.T) {
	var lastBody, lastPrefer string
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		lastPrefer = strings.Join(r.Header.Values("Prefer"), ",")
		switch {
		case r.URL.Path == "/v1.0/me/calendars":
			w.Write([]byte(`{"value":[{"id":"c1","name":"Calendar","isDefaultCalendar":true,"canEdit":true,"hexColor":"#0078d4"},{"id":"c2","name":"Birthdays","canEdit":false}]}`))
		case strings.HasPrefix(r.URL.Path, "/v1.0/me/calendars/c1/calendarView"):
			if r.URL.Query().Get("startDateTime") == "" {
				t.Errorf("missing range: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"value":[
				{"id":"o1","subject":"1:1 with Kevin","start":{"dateTime":"2026-10-27T10:00:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-10-27T10:30:00.0000000","timeZone":"UTC"},"isOrganizer":true,"seriesMasterId":"s1","webLink":"https://outlook/o1",
				  "attendees":[{"emailAddress":{"name":"Kevin","address":"kevin@x.com"},"status":{"response":"accepted"}}],"organizer":{"emailAddress":{"address":"jacob@outlook.com"}},"responseStatus":{"response":"organizer"}},
				{"id":"o2","subject":"Kickoff","start":{"dateTime":"2026-10-29T18:00:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-10-29T19:00:00.0000000","timeZone":"UTC"},"isOrganizer":false,"location":{"displayName":"Teams"},"onlineMeeting":{"joinUrl":"https://teams/x"},
				  "attendees":[{"emailAddress":{"address":"priya@x.com"},"status":{"response":"accepted"}},{"emailAddress":{"address":"jacob@outlook.com"},"status":{"response":"notResponded"}}],"organizer":{"emailAddress":{"address":"priya@x.com"}},"responseStatus":{"response":"notResponded"}},
				{"id":"o3","subject":"Cancelled","isCancelled":true},
				{"id":"o4","subject":"Report due","isAllDay":true,"start":{"dateTime":"2026-10-28T00:00:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-10-29T00:00:00.0000000","timeZone":"UTC"},"isOrganizer":true}]}`))
		case strings.HasPrefix(r.URL.Path, "/v1.0/me/calendars/c2/calendarView"):
			w.Write([]byte(`{"value":[]}`))
		case r.URL.Path == "/v1.0/me/calendars/c1/events" && r.Method == "POST":
			w.Write([]byte(`{"id":"new1","subject":"Lunch","start":{"dateTime":"2026-10-28T12:00:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-10-28T13:00:00.0000000","timeZone":"UTC"},"isOrganizer":true}`))
		case r.URL.Path == "/v1.0/me/events/o1" && r.Method == "PATCH":
			w.Write([]byte(`{"id":"o1","subject":"Renamed","isOrganizer":true,"start":{"dateTime":"2026-10-27T10:00:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-10-27T10:30:00.0000000","timeZone":"UTC"}}`))
		case r.URL.Path == "/v1.0/me/events/o2/tentativelyAccept":
			w.WriteHeader(202)
		case r.URL.Path == "/v1.0/me/events/o1" && r.Method == "DELETE":
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	ctx := context.Background()
	cals, err := c.Calendars(ctx)
	if err != nil || len(cals) != 2 || !cals[0].Primary || cals[1].ReadOnly != true {
		t.Fatalf("calendars %+v %v", cals, err)
	}
	evs, err := c.Events(ctx, time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	if err != nil || len(evs) != 3 || !strings.Contains(lastPrefer, "outlook.timezone") {
		t.Fatalf("events: %d %v prefer=%s", len(evs), err, lastPrefer)
	}
	byID := map[string]cm.Event{}
	for _, e := range evs {
		byID[e.ID] = e
	}
	o1, o2, o4 := byID["o1"], byID["o2"], byID["o4"]
	if !o1.Mine || !o1.Recurring || o1.Attendees[0].Name != "Kevin" || o1.Start.UTC().Hour() != 10 || o1.MyStatus != "" {
		t.Fatalf("o1 %+v", o1)
	}
	if o2.Mine || o2.MyStatus != "needsAction" || o2.Online != "https://teams/x" || o2.Location != "Teams" || !o2.Attendees[0].Organizer {
		t.Fatalf("o2 %+v", o2)
	}
	if !o4.AllDay || o4.Start.Day() != 28 || o4.End.Day() != 28 {
		t.Fatalf("o4 %+v", o4)
	}
	ev, err := c.CreateEvent(ctx, cm.EventInput{CalendarID: "c1", Title: "Lunch", Start: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 28, 13, 0, 0, 0, time.UTC), Attendees: []cm.Address{{Email: "kevin@x.com"}}})
	if err != nil || ev.ID != "new1" {
		t.Fatalf("create %+v %v", ev, err)
	}
	var body map[string]any
	json.Unmarshal([]byte(lastBody), &body)
	if body["subject"] != "Lunch" || len(body["attendees"].([]any)) != 1 || body["start"].(map[string]any)["timeZone"] == "" {
		t.Fatalf("create body %s", lastBody)
	}
	if ev, err := c.UpdateEvent(ctx, "c1", "o1", cm.EventInput{Title: "Renamed", Start: o1.Start, End: o1.End}); err != nil || ev.Title != "Renamed" {
		t.Fatalf("update %+v %v", ev, err)
	}
	if err := c.RespondEvent(ctx, "c1", "o2", "tentative"); err != nil || lastBody != `{"sendResponse":true}` {
		t.Fatalf("respond %v %s", err, lastBody)
	}
	if err := c.DeleteEvent(ctx, "c1", "o1", true); err != nil {
		t.Fatal(err)
	}
}
