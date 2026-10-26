package demo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

// Sample calendars. Each Box has one calendar with a few events around
// today, so the calendar view has something to show.

func (b *Box) seedEvents(now time.Time) {
	day := func(offset int, h, m int) time.Time {
		d := now.AddDate(0, 0, offset)
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, time.Local)
	}
	me := mail.Attendee{Address: mail.Address{Name: "Jacob", Email: b.Email}, Status: "accepted", Organizer: true}
	add := func(e mail.Event) {
		b.nextEv++
		e.ID = "ev" + strconv.Itoa(b.nextEv)
		e.CalendarID = "primary"
		b.events = append(b.events, e)
	}
	if strings.Contains(b.Email, "gmail") {
		add(mail.Event{Title: "Support team standup", Start: day(0, 9, 0), End: day(0, 9, 15), Mine: true, Recurring: true, Attendees: []mail.Attendee{me, {Address: mail.Address{Name: "Kevin Brooks", Email: "kevin@credence.example"}, Status: "accepted"}, {Address: mail.Address{Name: "Ana Ruiz", Email: "ana@credence.example"}, Status: "accepted"}}})
		add(mail.Event{Title: "Call Maria about terminal 3", Location: "555-0134", Start: day(0, 11, 30), End: day(0, 12, 0), Mine: true, Notes: "Printer IP shows 0.0.0.0. Walk her through setting a static IP."})
		add(mail.Event{Title: "Zendesk SLA review", Start: day(1, 14, 0), End: day(1, 15, 0), Mine: true, Attendees: []mail.Attendee{me, {Address: mail.Address{Name: "Kevin Brooks", Email: "kevin@credence.example"}, Status: "needsAction"}}})
		add(mail.Event{Title: "Mom's birthday", Start: day(3, 0, 0), End: day(3, 0, 0), AllDay: true, Mine: true})
		add(mail.Event{Title: "Dentist", Location: "Main St Dental", Start: day(-2, 8, 0), End: day(-2, 9, 0), Mine: true})
		add(mail.Event{Title: "Sim racing league", Start: day(5, 20, 0), End: day(5, 22, 0), Mine: true, Recurring: true})
	} else {
		kick := nextThursday(now).Local()
		add(mail.Event{Title: "Onboarding kickoff - Toast dealer", Location: "Teams", Online: "https://teams.example/join/kickoff", Start: kick, End: kick.Add(time.Hour), Mine: false, MyStatus: "needsAction",
			Attendees: []mail.Attendee{{Address: mail.Address{Name: "Priya Nair", Email: "priya@restauranttech.example"}, Status: "accepted", Organizer: true}, {Address: mail.Address{Email: b.Email}, Status: "needsAction"}, {Address: mail.Address{Name: "Kevin Brooks", Email: "kevin@credence.example"}, Status: "accepted"}},
			Notes:     "Agenda: escalation matrix, hours, handoff."})
		add(mail.Event{Title: "Missed calls report due", Start: day(2, 0, 0), End: day(2, 0, 0), AllDay: true, Mine: true})
		add(mail.Event{Title: "1:1 with Kevin", Start: day(1, 10, 0), End: day(1, 10, 30), Mine: true, Recurring: true})
	}
}

func (b *Box) Calendars(context.Context) ([]mail.Calendar, error) {
	name := "Personal"
	if !strings.Contains(b.Email, "gmail") {
		name = "Calendar"
	}
	return []mail.Calendar{{ID: "primary", Name: name, Primary: true, Shown: true}}, nil
}

func (b *Box) Events(_ context.Context, from, to time.Time, only []string) ([]mail.Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if only != nil && len(only) == 0 {
		return nil, nil
	}
	var out []mail.Event
	for _, e := range b.events {
		end := e.End
		if e.AllDay {
			end = e.End.AddDate(0, 0, 1)
		}
		if e.Start.Before(to) && end.After(from) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (b *Box) CreateEvent(_ context.Context, in mail.EventInput) (*mail.Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextEv++
	e := fromInput(in, b.Email)
	e.ID = "ev" + strconv.Itoa(b.nextEv)
	e.CalendarID = "primary"
	b.events = append(b.events, e)
	return &e, nil
}

func (b *Box) UpdateEvent(_ context.Context, calID, id string, in mail.EventInput) (*mail.Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, e := range b.events {
		if e.ID == id {
			n := fromInput(in, b.Email)
			n.ID, n.CalendarID, n.Recurring = e.ID, e.CalendarID, e.Recurring
			b.events[i] = n
			return &n, nil
		}
	}
	return nil, fmt.Errorf("event not found")
}

func (b *Box) DeleteEvent(_ context.Context, calID, id string, notify bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, e := range b.events {
		if e.ID == id {
			b.events = append(b.events[:i], b.events[i+1:]...)
			return nil
		}
	}
	return nil
}

func (b *Box) RespondEvent(_ context.Context, calID, id, status string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := map[string]string{"ACCEPTED": "accepted", "TENTATIVE": "tentative", "DECLINED": "declined"}[strings.ToUpper(status)]
	if st == "" {
		return fmt.Errorf("unknown response %q", status)
	}
	for i := range b.events {
		if b.events[i].ID == id {
			b.events[i].MyStatus = st
			for j, a := range b.events[i].Attendees {
				if mail.SameEmail(a.Email, b.Email) {
					b.events[i].Attendees[j].Status = st
				}
			}
			return nil
		}
	}
	return fmt.Errorf("event not found")
}

func fromInput(in mail.EventInput, me string) mail.Event {
	e := mail.Event{Title: strings.TrimSpace(in.Title), Location: in.Location, Notes: in.Notes, Start: in.Start, End: in.End, AllDay: in.AllDay, Mine: true}
	if e.Title == "" {
		e.Title = "(no title)"
	}
	if len(in.Attendees) > 0 {
		e.Attendees = []mail.Attendee{{Address: mail.Address{Email: me}, Status: "accepted", Organizer: true}}
		for _, a := range in.Attendees {
			e.Attendees = append(e.Attendees, mail.Attendee{Address: a, Status: "needsAction"})
		}
	}
	return e
}
