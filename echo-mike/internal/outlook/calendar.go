package outlook

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

type gCalendar struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	HexColor  string `json:"hexColor"`
	IsDefault bool   `json:"isDefaultCalendar"`
	CanEdit   bool   `json:"canEdit"`
}

func (c *Client) Calendars(ctx context.Context) ([]cm.Calendar, error) {
	var out struct {
		Value []gCalendar `json:"value"`
	}
	if err := c.do(ctx, "GET", c.Base+"/me/calendars?$select=id,name,hexColor,isDefaultCalendar,canEdit", nil, &out); err != nil {
		return nil, err
	}
	var cals []cm.Calendar
	for _, g := range out.Value {
		cals = append(cals, cm.Calendar{ID: g.ID, Name: g.Name, Color: g.HexColor, Primary: g.IsDefault, ReadOnly: !g.CanEdit})
	}
	return cals, nil
}

type gDateTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type gEventAttendee struct {
	gAddr
	Status struct {
		Response string `json:"response"`
	} `json:"status"`
}

type gEvent struct {
	ID               string                        `json:"id"`
	Subject          string                        `json:"subject"`
	BodyPreview      string                        `json:"bodyPreview"`
	Start            gDateTime                     `json:"start"`
	End              gDateTime                     `json:"end"`
	IsAllDay         bool                          `json:"isAllDay"`
	IsCancelled      bool                          `json:"isCancelled"`
	IsOrganizer      bool                          `json:"isOrganizer"`
	Location         *struct{ DisplayName string } `json:"location"`
	Attendees        []gEventAttendee              `json:"attendees"`
	Organizer        *gAddr                        `json:"organizer"`
	ResponseStatus   *struct{ Response string }    `json:"responseStatus"`
	SeriesMasterID   string                        `json:"seriesMasterId"`
	WebLink          string                        `json:"webLink"`
	OnlineMeetingURL string                        `json:"onlineMeetingUrl"`
	OnlineMeeting    *struct {
		JoinURL string `json:"joinUrl"`
	} `json:"onlineMeeting"`
}

func (c *Client) event(g gEvent, calID string) cm.Event {
	e := cm.Event{ID: g.ID, CalendarID: calID, Title: g.Subject, Notes: g.BodyPreview, AllDay: g.IsAllDay, Mine: g.IsOrganizer, Recurring: g.SeriesMasterID != "", Link: g.WebLink, Online: g.OnlineMeetingURL}
	if e.Title == "" {
		e.Title = "(no title)"
	}
	if g.OnlineMeeting != nil && g.OnlineMeeting.JoinURL != "" {
		e.Online = g.OnlineMeeting.JoinURL
	}
	if g.Location != nil {
		e.Location = g.Location.DisplayName
	}
	e.Start = graphTime(g.Start.DateTime, g.Start.TimeZone)
	e.End = graphTime(g.End.DateTime, g.End.TimeZone)
	if g.IsAllDay {
		// Graph's all-day end is midnight after the last day.
		e.Start = time.Date(e.Start.Year(), e.Start.Month(), e.Start.Day(), 0, 0, 0, 0, time.Local)
		e.End = e.End.Add(-time.Second)
		e.End = time.Date(e.End.Year(), e.End.Month(), e.End.Day(), 0, 0, 0, 0, time.Local)
	}
	for _, a := range g.Attendees {
		e.Attendees = append(e.Attendees, cm.Attendee{Address: a.addr(), Status: graphStatus(a.Status.Response),
			Organizer: g.Organizer != nil && cm.SameEmail(a.EmailAddress.Address, g.Organizer.EmailAddress.Address)})
	}
	if g.ResponseStatus != nil && !g.IsOrganizer {
		e.MyStatus = graphStatus(g.ResponseStatus.Response)
	}
	return e
}

func graphStatus(s string) string {
	switch s {
	case "accepted", "organizer":
		return "accepted"
	case "tentativelyAccepted":
		return "tentative"
	case "declined":
		return "declined"
	case "notResponded":
		return "needsAction"
	}
	return ""
}

const eventFields = "id,subject,bodyPreview,start,end,isAllDay,isCancelled,isOrganizer,location,attendees,organizer,responseStatus,seriesMasterId,webLink,onlineMeetingUrl,onlineMeeting"

func (c *Client) calDo(ctx context.Context, method, rawURL string, body any, out any) error {
	// Dates come back in the local zone so the app doesn't have to convert.
	return c.doPrefer(ctx, method, rawURL, body, out, `outlook.timezone="`+localZone()+`"`)
}

func localZone() string {
	name := time.Local.String()
	if name == "Local" || name == "" {
		return "UTC"
	}
	return name
}

func (c *Client) Events(ctx context.Context, from, to time.Time) ([]cm.Event, error) {
	cals, err := c.Calendars(ctx)
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []cm.Event
	var firstErr error
	for _, cal := range cals {
		wg.Add(1)
		go func(cal cm.Calendar) {
			defer wg.Done()
			q := url.Values{"startDateTime": {from.UTC().Format(time.RFC3339)}, "endDateTime": {to.UTC().Format(time.RFC3339)}, "$select": {eventFields}, "$top": {"250"}, "$orderby": {"start/dateTime"}}
			var out struct {
				Value []gEvent `json:"value"`
			}
			err := c.calDo(ctx, "GET", c.Base+"/me/calendars/"+url.PathEscape(cal.ID)+"/calendarView?"+q.Encode(), nil, &out)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for _, g := range out.Value {
				if g.IsCancelled {
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

func graphBody(in cm.EventInput) map[string]any {
	zone := localZone()
	body := map[string]any{"subject": in.Title, "isAllDay": in.AllDay,
		"location": map[string]string{"displayName": in.Location},
		"body":     map[string]string{"contentType": "text", "content": in.Notes}}
	if in.AllDay {
		body["start"] = map[string]string{"dateTime": in.Start.Format("2006-01-02T00:00:00"), "timeZone": zone}
		body["end"] = map[string]string{"dateTime": in.End.AddDate(0, 0, 1).Format("2006-01-02T00:00:00"), "timeZone": zone}
	} else {
		body["start"] = map[string]string{"dateTime": in.Start.Local().Format("2006-01-02T15:04:05"), "timeZone": zone}
		body["end"] = map[string]string{"dateTime": in.End.Local().Format("2006-01-02T15:04:05"), "timeZone": zone}
	}
	att := []map[string]any{}
	for _, a := range in.Attendees {
		att = append(att, map[string]any{"type": "required", "emailAddress": map[string]string{"address": a.Email, "name": a.Name}})
	}
	body["attendees"] = att
	return body
}

func (c *Client) CreateEvent(ctx context.Context, in cm.EventInput) (*cm.Event, error) {
	path := c.Base + "/me/events"
	if in.CalendarID != "" {
		path = c.Base + "/me/calendars/" + url.PathEscape(in.CalendarID) + "/events"
	}
	var g gEvent
	if err := c.calDo(ctx, "POST", path, graphBody(in), &g); err != nil {
		return nil, err
	}
	e := c.event(g, in.CalendarID)
	return &e, nil
}

func (c *Client) UpdateEvent(ctx context.Context, calID, id string, in cm.EventInput) (*cm.Event, error) {
	var g gEvent
	if err := c.calDo(ctx, "PATCH", c.Base+"/me/events/"+url.PathEscape(id), graphBody(in), &g); err != nil {
		return nil, err
	}
	e := c.event(g, calID)
	return &e, nil
}

func (c *Client) DeleteEvent(ctx context.Context, calID, id string, notify bool) error {
	err := c.do(ctx, "DELETE", c.Base+"/me/events/"+url.PathEscape(id), nil, nil)
	var api *oauth.APIError
	if errors.As(err, &api) && api.Status == 404 {
		return nil
	}
	return err
}

func (c *Client) RespondEvent(ctx context.Context, calID, id, status string) error {
	verb := map[string]string{"ACCEPTED": "accept", "TENTATIVE": "tentativelyAccept", "DECLINED": "decline"}[strings.ToUpper(status)]
	if verb == "" {
		return fmt.Errorf("unknown response %q", status)
	}
	return c.do(ctx, "POST", c.Base+"/me/events/"+url.PathEscape(id)+"/"+verb, map[string]any{"sendResponse": true}, nil)
}
