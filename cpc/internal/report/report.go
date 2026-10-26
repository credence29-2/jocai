// Package report builds the one-page tech summary a support tech pastes
// into a ticket.
package report

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/credence29-2/jocai/cpc/internal/health"
)

// System is the PC and Windows summary.
type System struct {
	Computer   string `json:"computer"`
	User       string `json:"user"`
	Maker      string `json:"maker"`
	Model      string `json:"model"`
	Serial     string `json:"serial"`
	OS         string `json:"os"`
	Version    string `json:"version"`
	Build      string `json:"build"`
	Installed  string `json:"installed"`
	LastBoot   string `json:"lastBoot"`
	Uptime     string `json:"uptime"`
	LastUpdate string `json:"lastUpdate"`
	CPU        string `json:"cpu"`
	RAMBytes   int64  `json:"ram"`
	Domain     string `json:"domain"`
	IP         string `json:"ip"`
	Antivirus  string `json:"antivirus"`
	Activated  string `json:"activated"`
}

// Volume is one drive letter.
type Volume struct {
	Letter string `json:"letter"`
	Label  string `json:"label"`
	FS     string `json:"fs"`
	Size   int64  `json:"size"`
	Free   int64  `json:"free"`
}

// Item is a generic name/detail row.
type Item struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
	Flag   string `json:"flag,omitempty"` // good, warn, bad
}

// Clean is one past Kempt clean from the logs.
type Clean struct {
	When    string `json:"when"`
	Summary string `json:"summary"`
}

// Report is everything gathered.
type Report struct {
	App       string         `json:"app"`
	Version   string         `json:"version"`
	Company   string         `json:"company"`
	Generated time.Time      `json:"generated"`
	Elevated  bool           `json:"elevated"`
	System    System         `json:"system"`
	Volumes   []Volume       `json:"volumes"`
	Disks     []health.Disk  `json:"disks"`
	Battery   health.Battery `json:"battery"`
	Devices   []Item         `json:"devices"`
	Startup   []Item         `json:"startup"`
	Services  []Item         `json:"services"`
	Programs  []Item         `json:"programs"`
	Cleans    []Clean        `json:"cleans"`
	Notes     []string       `json:"notes"`
	Warnings  []string       `json:"warnings"`
}

func fmtBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Assess fills Warnings from the gathered data.
func (r *Report) Assess() {
	r.Warnings = nil
	for _, v := range r.Volumes {
		if v.Size > 0 && float64(v.Free)/float64(v.Size) < 0.10 {
			r.Warnings = append(r.Warnings, fmt.Sprintf("Drive %s is %.0f%% full (%s free).", v.Letter, 100*(1-float64(v.Free)/float64(v.Size)), fmtBytes(v.Free)))
		}
	}
	for _, d := range r.Disks {
		if d.Level == "bad" || d.Level == "warn" {
			r.Warnings = append(r.Warnings, d.Name+": "+d.Text)
		}
	}
	if r.Battery.Present && (r.Battery.Level == "bad" || r.Battery.Level == "warn") {
		r.Warnings = append(r.Warnings, "Battery: "+r.Battery.Text)
	}
	if n := len(r.Devices); n > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%d device(s) report a problem in Device Manager.", n))
	}
	on := 0
	for _, s := range r.Startup {
		if s.Flag != "off" {
			on++
		}
	}
	if on > 10 {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%d programs start with Windows.", on))
	}
	if r.System.LastUpdate != "" {
		if t, err := time.Parse("2006-01-02", r.System.LastUpdate); err == nil && r.Generated.Sub(t) > 90*24*time.Hour {
			r.Warnings = append(r.Warnings, "No Windows update installed in over 90 days.")
		}
	}
	if strings.Contains(strings.ToLower(r.System.Antivirus), "none") {
		r.Warnings = append(r.Warnings, "No antivirus reported to Windows Security.")
	}
	if strings.Contains(strings.ToLower(r.System.Activated), "not") {
		r.Warnings = append(r.Warnings, "Windows is not activated.")
	}
}

// ReadCleans summarizes the last n Kempt clean logs in dir.
func ReadCleans(dir string, n int) []Clean {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "clean-") && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	var out []Clean
	for _, name := range names {
		if len(out) >= n {
			break
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		last := ""
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.TrimSpace(lines[i]) != "" {
				last = strings.TrimSpace(lines[i])
				break
			}
		}
		when := strings.TrimSuffix(strings.TrimPrefix(name, "clean-"), ".log")
		if t, err := time.ParseInLocation("20060102-150405", when, time.Local); err == nil {
			when = t.Format("Jan 2, 2006 3:04 PM")
		}
		out = append(out, Clean{When: when, Summary: last})
	}
	return out
}

// Text renders the report as plain text for pasting into a ticket.
func (r *Report) Text() string {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\r\n", a...) }
	w("%s tech report  %s", r.App, r.Generated.Format("Jan 2, 2006 3:04 PM"))
	w("%s", strings.Repeat("=", 60))
	s := r.System
	w("Computer     %s  (user %s)", s.Computer, s.User)
	w("Make/model   %s %s  S/N %s", s.Maker, s.Model, s.Serial)
	w("Windows      %s  version %s  build %s", s.OS, s.Version, s.Build)
	w("Installed    %s   Last update %s   Activation: %s", s.Installed, s.LastUpdate, s.Activated)
	w("Last boot    %s   Uptime %s", s.LastBoot, s.Uptime)
	w("CPU / RAM    %s / %s", s.CPU, fmtBytes(s.RAMBytes))
	w("Network      %s   Domain/workgroup: %s", s.IP, s.Domain)
	w("Antivirus    %s", s.Antivirus)
	if len(r.Warnings) > 0 {
		w("")
		w("NEEDS ATTENTION")
		for _, x := range r.Warnings {
			w("  ! %s", x)
		}
	}
	w("")
	w("DRIVES")
	for _, v := range r.Volumes {
		pct := 0.0
		if v.Size > 0 {
			pct = 100 * (1 - float64(v.Free)/float64(v.Size))
		}
		w("  %s %-14s %8s total  %8s free  (%.0f%% used)", v.Letter, v.Label, fmtBytes(v.Size), fmtBytes(v.Free), pct)
	}
	for _, d := range r.Disks {
		extra := ""
		if d.Wear >= 0 {
			extra += fmt.Sprintf("  wear %d%%", d.Wear)
		}
		if d.PowerOnHours > 0 {
			extra += fmt.Sprintf("  %dh on", d.PowerOnHours)
		}
		w("  %s (%s, %s)  %s%s", d.Name, d.MediaType, fmtBytes(d.Size), d.Text, extra)
	}
	if r.Battery.Present {
		w("")
		w("BATTERY  %s  charge %d%%  %s", r.Battery.Status, r.Battery.Charge, r.Battery.Text)
	}
	section := func(title string, items []Item) {
		w("")
		w("%s (%d)", title, len(items))
		for _, it := range items {
			flag := ""
			if it.Flag != "" {
				flag = "[" + it.Flag + "] "
			}
			w("  %s%s  %s", flag, it.Name, it.Detail)
		}
	}
	if len(r.Devices) > 0 {
		section("DEVICES WITH PROBLEMS", r.Devices)
	}
	section("STARTUP PROGRAMS", r.Startup)
	section("THIRD-PARTY SERVICES", r.Services)
	section("INSTALLED PROGRAMS", r.Programs)
	if len(r.Cleans) > 0 {
		w("")
		w("RECENT %s CLEANS", strings.ToUpper(r.App))
		for _, c := range r.Cleans {
			w("  %s  %s", c.When, c.Summary)
		}
	}
	if len(r.Notes) > 0 {
		w("")
		for _, n := range r.Notes {
			w("Note: %s", n)
		}
	}
	w("")
	w("Generated by %s %s, %s", r.App, r.Version, r.Company)
	return b.String()
}

// HTML renders the report as a self-contained page.
func (r *Report) HTML() string {
	e := html.EscapeString
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	w(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>%s tech report, %s</title>`, e(r.App), e(r.System.Computer))
	w(`<style>body{font:14px/1.5 "Segoe UI",system-ui,sans-serif;color:#1a2330;background:#f4f6f9;margin:0;padding:28px}
.wrap{max-width:960px;margin:0 auto}h1{font-size:22px;margin:0 0 4px}h2{font-size:15px;margin:26px 0 8px;letter-spacing:.3px;text-transform:uppercase;color:#5b6773}
.sub{color:#5b6773;margin-bottom:18px}table{border-collapse:collapse;width:100%%;background:#fff;border:1px solid #e1e6ed;border-radius:8px;overflow:hidden}
td,th{padding:7px 12px;text-align:left;vertical-align:top;border-top:1px solid #eef1f5;font-size:13.5px}th{background:#f7f9fb;color:#5b6773;font-weight:600}tr:first-child td{border-top:0}
.kv td:first-child{width:170px;color:#5b6773}.warn{background:#fff7e6;border:1px solid #f3d39b;border-radius:8px;padding:12px 16px}.warn li{margin:3px 0}
.bad{color:#b42318;font-weight:600}.wrn{color:#b45309;font-weight:600}.good{color:#1b873f;font-weight:600}.foot{color:#8b96a3;font-size:12px;margin-top:28px}
.bar{height:8px;border-radius:4px;background:#e6eaf0;overflow:hidden;width:160px;display:inline-block;vertical-align:middle;margin-right:8px}.bar i{display:block;height:100%%;background:#2f7cf6}
@media print{body{background:#fff;padding:0}}</style></head><body><div class="wrap">`)
	w(`<h1>%s tech report</h1><div class="sub">%s &middot; %s &middot; generated %s%s</div>`, e(r.App), e(r.System.Computer), e(r.System.User), r.Generated.Format("Jan 2, 2006 3:04 PM"),
		map[bool]string{true: "", false: " &middot; run without administrator rights, some details limited"}[r.Elevated])
	if len(r.Warnings) > 0 {
		w(`<div class="warn"><b>Needs attention</b><ul>`)
		for _, x := range r.Warnings {
			w(`<li>%s</li>`, e(x))
		}
		w(`</ul></div>`)
	}
	s := r.System
	w(`<h2>System</h2><table class="kv">`)
	for _, kv := range [][2]string{{"Make and model", s.Maker + " " + s.Model}, {"Serial number", s.Serial}, {"Windows", s.OS + ", version " + s.Version + ", build " + s.Build},
		{"Windows installed", s.Installed}, {"Last update installed", s.LastUpdate}, {"Activation", s.Activated}, {"Last boot", s.LastBoot + " (up " + s.Uptime + ")"},
		{"Processor", s.CPU}, {"Memory", fmtBytes(s.RAMBytes)}, {"Network", s.IP}, {"Domain or workgroup", s.Domain}, {"Antivirus", s.Antivirus}} {
		if strings.TrimSpace(kv[1]) != "" {
			w(`<tr><td>%s</td><td>%s</td></tr>`, e(kv[0]), e(kv[1]))
		}
	}
	w(`</table>`)
	w(`<h2>Drives</h2><table><tr><th>Drive</th><th>Used</th><th>Free</th><th>Size</th></tr>`)
	for _, v := range r.Volumes {
		pct := 0.0
		if v.Size > 0 {
			pct = 100 * (1 - float64(v.Free)/float64(v.Size))
		}
		w(`<tr><td>%s %s</td><td><span class="bar"><i style="width:%.0f%%"></i></span>%.0f%%</td><td>%s</td><td>%s</td></tr>`, e(v.Letter), e(v.Label), pct, pct, fmtBytes(v.Free), fmtBytes(v.Size))
	}
	w(`</table>`)
	if len(r.Disks) > 0 {
		w(`<table style="margin-top:10px"><tr><th>Physical disk</th><th>Type</th><th>Size</th><th>Condition</th><th>Wear</th><th>Hours on</th></tr>`)
		for _, d := range r.Disks {
			cls := map[string]string{"good": "good", "warn": "wrn", "bad": "bad"}[d.Level]
			wear := "n/a"
			if d.Wear >= 0 {
				wear = fmt.Sprintf("%d%%", d.Wear)
			}
			w(`<tr><td>%s</td><td>%s</td><td>%s</td><td class="%s">%s</td><td>%s</td><td>%d</td></tr>`, e(d.Name), e(d.MediaType), fmtBytes(d.Size), cls, e(d.Text), wear, d.PowerOnHours)
		}
		w(`</table>`)
	}
	if r.Battery.Present {
		cls := map[string]string{"good": "good", "warn": "wrn", "bad": "bad"}[r.Battery.Level]
		w(`<h2>Battery</h2><table class="kv"><tr><td>Now</td><td>%s, %d%% charged</td></tr><tr><td>Condition</td><td class="%s">%s</td></tr></table>`, e(r.Battery.Status), r.Battery.Charge, cls, e(r.Battery.Text))
	}
	table := func(title string, items []Item, h1, h2 string) {
		w(`<h2>%s (%d)</h2>`, e(title), len(items))
		if len(items) == 0 {
			w(`<p class="sub">None.</p>`)
			return
		}
		w(`<table><tr><th>%s</th><th>%s</th></tr>`, e(h1), e(h2))
		for _, it := range items {
			cls := map[string]string{"good": "good", "warn": "wrn", "bad": "bad", "off": "sub"}[it.Flag]
			w(`<tr><td class="%s">%s</td><td>%s</td></tr>`, cls, e(it.Name), e(it.Detail))
		}
		w(`</table>`)
	}
	table("Devices with problems", r.Devices, "Device", "Problem")
	table("Startup programs", r.Startup, "Program", "Command")
	table("Third-party services", r.Services, "Service", "Start type and state")
	table("Installed programs", r.Programs, "Program", "Version and publisher")
	if len(r.Cleans) > 0 {
		w(`<h2>Recent %s cleans</h2><table><tr><th>When</th><th>Result</th></tr>`, e(r.App))
		for _, c := range r.Cleans {
			w(`<tr><td>%s</td><td>%s</td></tr>`, e(c.When), e(c.Summary))
		}
		w(`</table>`)
	}
	for _, n := range r.Notes {
		w(`<p class="sub">%s</p>`, e(n))
	}
	w(`<div class="foot">Generated by %s %s, %s.</div></div></body></html>`, e(r.App), e(r.Version), e(r.Company))
	return b.String()
}
