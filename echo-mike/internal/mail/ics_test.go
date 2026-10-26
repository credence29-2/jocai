package mail

import (
	"strings"
	"testing"
	"time"
)

const sampleICS = "BEGIN:VCALENDAR\r\nPRODID:-//Microsoft Corporation//Outlook 16.0 MIMEDIR//EN\r\nVERSION:2.0\r\nMETHOD:REQUEST\r\n" +
	"BEGIN:VEVENT\r\nUID:040000008200E00074C5B7101A82E00800000000@example\r\nSEQUENCE:2\r\n" +
	"ORGANIZER;CN=\"Nair, Priya\":mailto:priya@restauranttech.example\r\n" +
	"ATTENDEE;CN=Jacob;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:\r\n jacob.demo@outlook.example\r\n" +
	"ATTENDEE;CN=Kevin;PARTSTAT=ACCEPTED:mailto:kevin@credence.example\r\n" +
	"SUMMARY:Onboarding kickoff\\, Toast dealer\r\nLOCATION:Teams\r\nDESCRIPTION:Agenda:\\n- escalation matrix\\n- hours\r\n" +
	"DTSTART;TZID=\"Eastern Standard Time\":20261029T140000\r\nDTEND;TZID=\"Eastern Standard Time\":20261029T150000\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR\r\n"

func TestParseICS(t *testing.T) {
	inv, err := ParseICS(sampleICS, "Jacob.Demo@outlook.example")
	if err != nil {
		t.Fatal(err)
	}
	if inv.Summary != "Onboarding kickoff, Toast dealer" || inv.Location != "Teams" || inv.Method != "REQUEST" || inv.Sequence != 2 {
		t.Fatalf("parsed %+v", inv)
	}
	if inv.Organizer.Email != "priya@restauranttech.example" || inv.Organizer.Name != "Nair, Priya" {
		t.Fatalf("organizer %+v", inv.Organizer)
	}
	if len(inv.Attendees) != 2 || inv.Attendees[0].Email != "jacob.demo@outlook.example" || inv.MyStatus != "NEEDS-ACTION" {
		t.Fatalf("attendees %+v status %q", inv.Attendees, inv.MyStatus)
	}
	ny, _ := time.LoadLocation("America/New_York")
	if !inv.Start.Equal(time.Date(2026, 10, 29, 14, 0, 0, 0, ny)) || inv.End.Sub(inv.Start) != time.Hour || inv.AllDay {
		t.Fatalf("time %v to %v", inv.Start, inv.End)
	}
	if !strings.Contains(inv.Description, "- escalation matrix\n") {
		t.Fatalf("description %q", inv.Description)
	}

	allDay, err := ParseICS("BEGIN:VCALENDAR\nMETHOD:CANCEL\nBEGIN:VEVENT\nUID:x\nSUMMARY:Holiday\nDTSTART;VALUE=DATE:20261225\nEND:VEVENT\nEND:VCALENDAR", "")
	if err != nil || !allDay.AllDay || allDay.Method != "CANCEL" || allDay.End.Sub(allDay.Start) != 24*time.Hour {
		t.Fatalf("all day: %+v %v", allDay, err)
	}
	if _, err := ParseICS("BEGIN:VCALENDAR\nEND:VCALENDAR", ""); err == nil {
		t.Fatal("no event should fail")
	}
}

func TestReplyICS(t *testing.T) {
	inv, _ := ParseICS(sampleICS, "")
	out := ReplyICS(inv, Address{Name: "Jacob Oyler", Email: "jacob.demo@outlook.example"}, "accepted", time.Date(2026, 10, 26, 5, 0, 0, 0, time.UTC))
	for _, want := range []string{"METHOD:REPLY", "UID:040000008200E00074C5B7101A82E00800000000@example", "ATTENDEE;PARTSTAT=ACCEPTED;CN=Jacob Oyler:mailto:jacob.demo@outlook.example",
		"ORGANIZER:mailto:priya@restauranttech.example", "SUMMARY:Onboarding kickoff\\, Toast dealer", "DTSTART:20261029T180000Z", "SEQUENCE:2", "DTSTAMP:20261026T050000Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	// Lines are folded under 76 bytes and the reply parses back.
	for _, l := range strings.Split(out, "\r\n") {
		if len(l) > 75 {
			t.Errorf("long line: %q", l)
		}
	}
	back, err := ParseICS(out, "jacob.demo@outlook.example")
	if err != nil || back.Method != "REPLY" || back.MyStatus != "ACCEPTED" {
		t.Fatalf("round trip: %+v %v", back, err)
	}
	if ReplyWords(inv, "TENTATIVE") != "Tentatively accepted: Onboarding kickoff, Toast dealer" {
		t.Fatal(ReplyWords(inv, "TENTATIVE"))
	}
}
