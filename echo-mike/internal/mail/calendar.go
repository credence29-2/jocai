package mail

import (
	"context"
	"errors"
	"time"
)

// Calendar is one calendar in an account.
type Calendar struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color,omitempty"`
	Primary  bool   `json:"primary,omitempty"`
	ReadOnly bool   `json:"readOnly,omitempty"`
	// Shown is whether the provider's own calendar app shows it (Google's
	// "selected" tick). Shared calendars you've hidden there stay hidden
	// here until you tick them.
	Shown bool `json:"shown"`
}

// Attendee is someone invited to an event, with their answer.
type Attendee struct {
	Address
	// Status is accepted, tentative, declined or needsAction.
	Status string `json:"status,omitempty"`
	// Organizer marks the person who owns the event.
	Organizer bool `json:"organizer,omitempty"`
}

// Event is one calendar entry. Recurring series come back as their
// occurrences, each with its own ID.
type Event struct {
	ID         string     `json:"id"`
	CalendarID string     `json:"calendarId"`
	AccountID  string     `json:"accountId,omitempty"`
	Title      string     `json:"title"`
	Location   string     `json:"location,omitempty"`
	Notes      string     `json:"notes,omitempty"`
	Start      time.Time  `json:"start"`
	End        time.Time  `json:"end"`
	AllDay     bool       `json:"allDay"`
	Attendees  []Attendee `json:"attendees,omitempty"`
	// Mine is true when this mailbox organized the event, so it can be
	// edited; otherwise it can only be answered.
	Mine bool `json:"mine"`
	// MyStatus is this mailbox's answer when invited.
	MyStatus  string `json:"myStatus,omitempty"`
	Recurring bool   `json:"recurring,omitempty"`
	Link      string `json:"link,omitempty"`
	Online    string `json:"online,omitempty"`
}

// EventInput is what the New and Edit dialogs send.
type EventInput struct {
	CalendarID string    `json:"calendarId"`
	Title      string    `json:"title"`
	Location   string    `json:"location,omitempty"`
	Notes      string    `json:"notes,omitempty"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	AllDay     bool      `json:"allDay"`
	Attendees  []Address `json:"attendees,omitempty"`
	// Notify sends invitations and updates to attendees.
	Notify bool `json:"notify"`
}

// CalendarProvider is a mail provider that also has calendars. The Gmail
// and Outlook clients implement it.
type CalendarProvider interface {
	Calendars(ctx context.Context) ([]Calendar, error)
	// Events lists every occurrence that overlaps [from, to) in the given
	// calendars, or in every shown calendar when calendars is nil.
	Events(ctx context.Context, from, to time.Time, calendars []string) ([]Event, error)
	CreateEvent(ctx context.Context, in EventInput) (*Event, error)
	UpdateEvent(ctx context.Context, calID, id string, in EventInput) (*Event, error)
	DeleteEvent(ctx context.Context, calID, id string, notify bool) error
	// RespondEvent answers an invitation: accepted, tentative or declined.
	RespondEvent(ctx context.Context, calID, id, status string) error
}

// ErrCalendarScope means the account was connected before the app asked
// for calendar access; reconnecting it fixes that.
var ErrCalendarScope = errors.New("this account needs to be reconnected to allow calendar access")
