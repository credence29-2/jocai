package gmail

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	cm "github.com/credence29-2/jocai/echo-mike/internal/mail"
	"github.com/credence29-2/jocai/echo-mike/internal/oauth"
)

// CalendarScope is asked for at sign-in alongside Gmail. Accounts connected
// before it was added get ErrCalendarScope until reconnected.
const CalendarScope = "https://www.googleapis.com/auth/calendar"

// calBase is the Calendar API host. It's the same host as Gmail's in tests,
// where Base points at a fake server.
func (c *Client) calBase() string {
	if strings.Contains(c.Base, "googleapis.com") {
		return "https://www.googleapis.com"
	}
	return c.Base
}

func (c *Client) calDo(ctx context.Context, method, path string, body any, out any) error {
	err := c.doURL(ctx, method, c.calBase()+path, body, out)
	var api *oauth.APIError
	if errors.As(err, &api) && (api.Status == 403 || api.Status == 401) {
		low := strings.ToLower(api.Msg)
		if strings.Contains(low, "insufficient") || strings.Contains(low, "scope") {
			return cm.ErrCalendarScope
		}
	}
	return err
}

type gcal struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	SummaryOverride string `json:"summaryOverride"`
	BackgroundColor string `json:"backgroundColor"`
	Primary         bool   `json:"primary"`
	AccessRole      string `json:"accessRole"`
	Selected        bool   `json:"selected"`
	Hidden          bool   `json:"hidden"`
}

func (c *Client) Calendars(ctx context.Context) ([]cm.Calendar, error) {
	var out struct {
		Items []gcal `json:"items"`
	}
	if err := c.calDo(ctx, "GET", "/calendar/v3/users/me/calendarList?minAccessRole=reader&showHidden=false", nil, &out); err != nil {
		return nil, err
	}
	var cals []cm.Calendar
	for _, g := range out.Items {
		if g.Hidden {
			continue
		}
		name := g.SummaryOverride
		if name == "" {
			name = g.Summary
		}
		cals = append(cals, cm.Calendar{ID: g.ID, Name: name, Color: g.BackgroundColor, Primary: g.Primary, ReadOnly: g.AccessRole != "owner" && g.AccessRole != "writer", Shown: g.Selected || g.Primary})
	}
	return cals, nil
}

type gTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type gAttendee struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	Organizer      bool   `json:"organizer,omitempty"`
	Self           bool   `json:"self,omitempty"`
}

type gEvent struct {
	ID          string      `json:"id"`
	Status      string      `json:"status"`
	EventType   string      `json:"eventType"`
	Summary     string      `json:"summary"`
	Location    string      `json:"location"`
	Description string      `json:"description"`
	Start       gTime       `json:"start"`
	End         gTime       `json:"end"`
	Attendees   []gAttendee `json:"attendees"`
	Organizer   *gAttendee  `json:"organizer"`
	RecurringID string      `json:"recurringEventId"`
	HTMLLink    string      `json:"htmlLink"`
	HangoutLink string      `json:"hangoutLink"`
}

func (c *Client) event(g gEvent, calID string) cm.Event {
	e := cm.Event{ID: g.ID, CalendarID: calID, Title: g.Summary, Location: g.Location, Notes: g.Description, Recurring: g.RecurringID != "", Link: g.HTMLLink, Online: g.HangoutLink}
	if e.Title == "" {
		e.Title = "(no title)"
	}
	e.Start, e.AllDay = gcalTime(g.Start)
	e.End, _ = gcalTime(g.End)
	// Google's all-day end date is the day after; the app wants the last day.
	e.Mine = g.Organizer == nil || g.Organizer.Self || cm.SameEmail(g.Organizer.Email, c.email)
	for _, a := range g.Attendees {
		e.Attendees = append(e.Attendees, cm.Attendee{Address: cm.Address{Name: a.DisplayName, Email: a.Email}, Status: a.ResponseStatus, Organizer: a.Organizer})
		if a.Self || cm.SameEmail(a.Email, c.email) {
			e.MyStatus = a.ResponseStatus
		}
	}
	return e
}

func gcalTime(t gTime) (time.Time, bool) {
	if t.Date != "" {
		d, _ := time.ParseInLocation("2006-01-02", t.Date, time.Local)
		return d, true
	}
	v, err := time.Parse(time.RFC3339, t.DateTime)
	if err != nil {
		return time.Time{}, false
	}
	return v.Local(), false
}

func (c *Client) Events(ctx context.Context, from, to time.Time, only []string) ([]cm.Event, error) {
	cals, err := c.Calendars(ctx)
	if err != nil {
		return nil, err
	}
	cals = pickCalendars(cals, only)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []cm.Event
	var firstErr error
	for _, cal := range cals {
		wg.Add(1)
		go func(cal cm.Calendar) {
			defer wg.Done()
			q := url.Values{"timeMin": {from.UTC().Format(time.RFC3339)}, "timeMax": {to.UTC().Format(time.RFC3339)}, "singleEvents": {"true"}, "orderBy": {"startTime"}, "maxResults": {"250"}}
			var out struct {
				Items []gEvent `json:"items"`
			}
			err := c.calDo(ctx, "GET", "/calendar/v3/calendars/"+url.PathEscape(cal.ID)+"/events?"+q.Encode(), nil, &out)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for _, g := range out.Items {
				// Working-location and focus-time entries are Google's own
				// noise, not appointments.
				if g.Status == "cancelled" || g.EventType == "workingLocation" || g.EventType == "focusTime" {
					continue
				}
				e := c.event(g, cal.ID)
				if cal.ReadOnly {
					e.Mine = false
				}
				all = append(all, e)
			}
		}(cal)
	}
	wg.Wait()
	if all == nil && firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

func gcalBody(in cm.EventInput) map[string]any {
	body := map[string]any{"summary": in.Title, "location": in.Location, "description": in.Notes}
	if in.AllDay {
		body["start"] = map[string]string{"date": in.Start.Format("2006-01-02")}
		body["end"] = map[string]string{"date": in.End.AddDate(0, 0, 1).Format("2006-01-02")}
	} else {
		body["start"] = map[string]string{"dateTime": in.Start.Format(time.RFC3339)}
		body["end"] = map[string]string{"dateTime": in.End.Format(time.RFC3339)}
	}
	att := []map[string]string{}
	for _, a := range in.Attendees {
		m := map[string]string{"email": a.Email}
		if a.Name != "" {
			m["displayName"] = a.Name
		}
		att = append(att, m)
	}
	body["attendees"] = att
	return body
}

func sendUpdates(notify bool) string {
	if notify {
		return "all"
	}
	return "none"
}

func (c *Client) CreateEvent(ctx context.Context, in cm.EventInput) (*cm.Event, error) {
	calID := in.CalendarID
	if calID == "" {
		calID = "primary"
	}
	var g gEvent
	if err := c.calDo(ctx, "POST", "/calendar/v3/calendars/"+url.PathEscape(calID)+"/events?sendUpdates="+sendUpdates(in.Notify), gcalBody(in), &g); err != nil {
		return nil, err
	}
	e := c.event(g, calID)
	return &e, nil
}

func (c *Client) UpdateEvent(ctx context.Context, calID, id string, in cm.EventInput) (*cm.Event, error) {
	var g gEvent
	if err := c.calDo(ctx, "PATCH", "/calendar/v3/calendars/"+url.PathEscape(calID)+"/events/"+url.PathEscape(id)+"?sendUpdates="+sendUpdates(in.Notify), gcalBody(in), &g); err != nil {
		return nil, err
	}
	e := c.event(g, calID)
	return &e, nil
}

func (c *Client) DeleteEvent(ctx context.Context, calID, id string, notify bool) error {
	err := c.calDo(ctx, "DELETE", "/calendar/v3/calendars/"+url.PathEscape(calID)+"/events/"+url.PathEscape(id)+"?sendUpdates="+sendUpdates(notify), nil, nil)
	var api *oauth.APIError
	if errors.As(err, &api) && (api.Status == 404 || api.Status == 410) {
		return nil
	}
	return err
}

func (c *Client) RespondEvent(ctx context.Context, calID, id, status string) error {
	st := map[string]string{"ACCEPTED": "accepted", "TENTATIVE": "tentative", "DECLINED": "declined"}[strings.ToUpper(status)]
	if st == "" {
		return fmt.Errorf("unknown response %q", status)
	}
	// Only this mailbox's attendee line changes; everything else stays.
	var g gEvent
	path := "/calendar/v3/calendars/" + url.PathEscape(calID) + "/events/" + url.PathEscape(id)
	if err := c.calDo(ctx, "GET", path, nil, &g); err != nil {
		return err
	}
	var att []map[string]any
	found := false
	for _, a := range g.Attendees {
		m := map[string]any{"email": a.Email, "responseStatus": a.ResponseStatus}
		if a.Self || cm.SameEmail(a.Email, c.email) {
			m["responseStatus"] = st
			found = true
		}
		att = append(att, m)
	}
	if !found {
		att = append(att, map[string]any{"email": c.email, "responseStatus": st})
	}
	return c.calDo(ctx, "PATCH", path+"?sendUpdates=all", map[string]any{"attendees": att}, nil)
}

// pickCalendars keeps the calendars asked for, or the shown ones.
func pickCalendars(cals []cm.Calendar, only []string) []cm.Calendar {
	var out []cm.Calendar
	for _, c := range cals {
		if only == nil {
			if c.Shown {
				out = append(out, c)
			}
			continue
		}
		for _, id := range only {
			if id == c.ID {
				out = append(out, c)
			}
		}
	}
	return out
}
