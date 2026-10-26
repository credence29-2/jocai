package mail

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Invite is a calendar invitation found in a message: the one VEVENT of a
// text/calendar part with METHOD:REQUEST or CANCEL.
type Invite struct {
	UID         string    `json:"uid"`
	Method      string    `json:"method"` // REQUEST, CANCEL, REPLY
	Summary     string    `json:"summary"`
	Description string    `json:"description,omitempty"`
	Location    string    `json:"location,omitempty"`
	Organizer   Address   `json:"organizer"`
	Attendees   []Address `json:"attendees,omitempty"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	AllDay      bool      `json:"allDay"`
	Sequence    int       `json:"sequence"`
	// MyStatus is this mailbox's PARTSTAT in the invite, if listed.
	MyStatus string `json:"myStatus,omitempty"`
}

// ParseICS reads the first VEVENT of an iCalendar text. me, when set, is
// used to find this mailbox's own attendee line.
func ParseICS(text, me string) (*Invite, error) {
	lines := unfold(text)
	inv := &Invite{}
	inEvent := false
	for _, line := range lines {
		name, params, value := icsLine(line)
		switch {
		case name == "BEGIN" && value == "VEVENT":
			if inv.UID != "" {
				continue // only the first event
			}
			inEvent = true
		case name == "END" && value == "VEVENT":
			inEvent = false
		case name == "METHOD":
			inv.Method = strings.ToUpper(value)
		}
		if !inEvent {
			continue
		}
		switch name {
		case "UID":
			inv.UID = value
		case "SUMMARY":
			inv.Summary = unescape(value)
		case "DESCRIPTION":
			inv.Description = unescape(value)
		case "LOCATION":
			inv.Location = unescape(value)
		case "SEQUENCE":
			inv.Sequence, _ = strconv.Atoi(value)
		case "ORGANIZER":
			inv.Organizer = calAddr(params, value)
		case "ATTENDEE":
			a := calAddr(params, value)
			inv.Attendees = append(inv.Attendees, a)
			if me != "" && SameEmail(a.Email, me) {
				inv.MyStatus = strings.ToUpper(params["PARTSTAT"])
			}
		case "DTSTART":
			inv.Start, inv.AllDay = calTime(params, value)
		case "DTEND":
			inv.End, _ = calTime(params, value)
		}
	}
	if inv.UID == "" || inv.Start.IsZero() {
		return nil, fmt.Errorf("no event in the calendar part")
	}
	if inv.End.IsZero() {
		inv.End = inv.Start.Add(time.Hour)
		if inv.AllDay {
			inv.End = inv.Start.Add(24 * time.Hour)
		}
	}
	if inv.Method == "" {
		inv.Method = "REQUEST"
	}
	return inv, nil
}

// ReplyICS builds the calendar reply that tells the organizer whether you're
// coming. status is ACCEPTED, TENTATIVE or DECLINED.
func ReplyICS(inv *Invite, me Address, status string, now time.Time) string {
	status = strings.ToUpper(status)
	cn := me.Name
	if cn == "" {
		cn = me.Email
	}
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\r\n") }
	w("BEGIN:VCALENDAR")
	w("PRODID:-//Project Echo Mike//EN")
	w("VERSION:2.0")
	w("METHOD:REPLY")
	w("BEGIN:VEVENT")
	w("UID:" + inv.UID)
	w("DTSTAMP:" + now.UTC().Format("20060102T150405Z"))
	w("ORGANIZER:mailto:" + inv.Organizer.Email)
	w(fold(fmt.Sprintf("ATTENDEE;PARTSTAT=%s;CN=%s:mailto:%s", status, escape(cn), me.Email)))
	w(fold("SUMMARY:" + escape(inv.Summary)))
	if inv.AllDay {
		w("DTSTART;VALUE=DATE:" + inv.Start.Format("20060102"))
	} else {
		w("DTSTART:" + inv.Start.UTC().Format("20060102T150405Z"))
	}
	w("SEQUENCE:" + strconv.Itoa(inv.Sequence))
	w("END:VEVENT")
	w("END:VCALENDAR")
	return b.String()
}

// ReplyWords is the short message that goes with a calendar reply.
func ReplyWords(inv *Invite, status string) string {
	verb := map[string]string{"ACCEPTED": "accepted", "TENTATIVE": "tentatively accepted", "DECLINED": "declined"}[strings.ToUpper(status)]
	return fmt.Sprintf("%s: %s", strings.ToUpper(verb[:1])+verb[1:], inv.Summary)
}

func unfold(text string) []string {
	raw := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out []string
	for _, l := range raw {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		out = append(out, l)
	}
	return out
}

// icsLine splits "NAME;P1=V1;P2=V2:value".
func icsLine(line string) (name string, params map[string]string, value string) {
	params = map[string]string{}
	i := strings.IndexByte(line, ':')
	// A ':' can appear inside a quoted parameter value.
	inQuote := false
	for j, r := range line {
		if r == '"' {
			inQuote = !inQuote
		}
		if r == ':' && !inQuote {
			i = j
			break
		}
	}
	if i < 0 {
		return strings.ToUpper(line), params, ""
	}
	head, value := line[:i], line[i+1:]
	parts := strings.Split(head, ";")
	name = strings.ToUpper(parts[0])
	for _, p := range parts[1:] {
		k, v, _ := strings.Cut(p, "=")
		params[strings.ToUpper(k)] = strings.Trim(v, `"`)
	}
	return name, params, value
}

func calAddr(params map[string]string, value string) Address {
	email := strings.TrimPrefix(strings.TrimPrefix(value, "mailto:"), "MAILTO:")
	return Address{Name: params["CN"], Email: strings.TrimSpace(email)}
}

// windowsZones maps the Windows time zone names Outlook puts in TZID to
// IANA names. Others fall back to the local zone.
var windowsZones = map[string]string{
	"Eastern Standard Time": "America/New_York", "Central Standard Time": "America/Chicago",
	"Mountain Standard Time": "America/Denver", "Pacific Standard Time": "America/Los_Angeles",
	"US Mountain Standard Time": "America/Phoenix", "Alaskan Standard Time": "America/Anchorage",
	"Hawaiian Standard Time": "Pacific/Honolulu", "Atlantic Standard Time": "America/Halifax",
	"GMT Standard Time": "Europe/London", "W. Europe Standard Time": "Europe/Berlin",
	"Romance Standard Time": "Europe/Paris", "Central European Standard Time": "Europe/Warsaw",
	"India Standard Time": "Asia/Kolkata", "AUS Eastern Standard Time": "Australia/Sydney",
	"Tokyo Standard Time": "Asia/Tokyo", "China Standard Time": "Asia/Shanghai",
}

func calTime(params map[string]string, value string) (time.Time, bool) {
	if params["VALUE"] == "DATE" || len(value) == 8 {
		t, err := time.ParseInLocation("20060102", value, time.Local)
		return t, err == nil
	}
	if strings.HasSuffix(value, "Z") {
		t, _ := time.Parse("20060102T150405Z", value)
		return t, false
	}
	loc := time.Local
	if tz := params["TZID"]; tz != "" {
		name := tz
		if iana, ok := windowsZones[tz]; ok {
			name = iana
		}
		if l, err := time.LoadLocation(name); err == nil {
			loc = l
		}
	}
	t, _ := time.ParseInLocation("20060102T150405", value, loc)
	return t, false
}

func unescape(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return r.Replace(s)
}

func escape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "\n", `\n`, ",", `\,`, ";", `\;`)
	return r.Replace(s)
}

// fold wraps a line at 75 bytes, as iCalendar requires.
func fold(s string) string {
	if len(s) <= 75 {
		return s
	}
	var b strings.Builder
	for len(s) > 75 {
		b.WriteString(s[:75] + "\r\n ")
		s = s[75:]
	}
	b.WriteString(s)
	return b.String()
}
