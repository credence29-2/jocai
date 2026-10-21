package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/credence29-2/jocai/cpc/internal/clean"
	"github.com/credence29-2/jocai/cpc/internal/settings"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

var processNames = map[string]string{
	"chrome.exe":   "Google Chrome",
	"msedge.exe":   "Microsoft Edge",
	"brave.exe":    "Brave",
	"vivaldi.exe":  "Vivaldi",
	"firefox.exe":  "Firefox",
	"teams.exe":    "Microsoft Teams (classic)",
	"ms-teams.exe": "Microsoft Teams",
	"discord.exe":  "Discord",
	"spotify.exe":  "Spotify",
	"zoom.exe":     "Zoom",
}

func friendly(exe string) string {
	if n, ok := processNames[exe]; ok {
		return n
	}
	return exe
}

type cleanRow struct {
	t       *clean.Target
	checked bool
	found   string
	result  string
}

// cleanModel backs the checkable cleaner table.
type cleanModel struct {
	walk.TableModelBase
	rows     []*cleanRow
	elevated bool
	locked   bool // true while a scan or clean runs
	onChange func()
	later    func(func())
}

func (m *cleanModel) RowCount() int { return len(m.rows) }

func (m *cleanModel) Value(row, col int) interface{} {
	r := m.rows[row]
	switch col {
	case 0:
		return r.t.Name
	case 1:
		return r.t.Section
	case 2:
		return r.found
	case 3:
		if !clean.Selectable(r.t, m.elevated) {
			return "Needs administrator"
		}
		return r.result
	}
	return ""
}

func (m *cleanModel) Checked(row int) bool { return m.rows[row].checked }

func (m *cleanModel) SetChecked(row int, checked bool) error {
	r := m.rows[row]
	if m.locked || (checked && !clean.Selectable(r.t, m.elevated)) {
		// Refuse the change and redraw the row with its real state once
		// the table is done handling the click.
		if m.later != nil {
			m.later(func() { m.PublishRowChanged(row) })
		}
		return nil
	}
	r.checked = checked
	if m.onChange != nil {
		m.onChange()
	}
	return nil
}

type cleanerTab struct {
	a     *app
	eng   *clean.Engine
	model *cleanModel

	table    *walk.TableView
	desc     *walk.Label
	status   *walk.Label
	progress *walk.ProgressBar
	analyze  *walk.PushButton
	run      *walk.PushButton
	stop     *walk.PushButton
	openLog  *walk.PushButton
	lastLog  string

	mu       sync.Mutex
	cancelFn context.CancelFunc
}

func newCleanerTab(a *app) *cleanerTab {
	eng := clean.NewEngine()
	eng.Elevated = a.elevated
	c := &cleanerTab{a: a, eng: eng, model: &cleanModel{elevated: a.elevated}}
	for _, t := range clean.AllTargets() {
		checked := a.cfg.IsSelected(t.ID, t.Default) && clean.Selectable(t, a.elevated)
		c.model.rows = append(c.model.rows, &cleanRow{t: t, checked: checked})
	}
	return c
}

func (c *cleanerTab) page() TabPage {
	return TabPage{
		Title:  "Cleaner",
		Layout: VBox{},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					PushButton{Text: "Recommended", ToolTipText: "Tick the safe default items", OnClicked: func() { c.selectWhere(func(t *clean.Target) bool { return t.Default }) }},
					PushButton{Text: "Select all", OnClicked: func() { c.selectWhere(func(*clean.Target) bool { return true }) }},
					PushButton{Text: "Select none", OnClicked: func() { c.selectWhere(func(*clean.Target) bool { return false }) }},
					HSpacer{},
				},
			},
			TableView{
				AssignTo:                 &c.table,
				CheckBoxes:               true,
				AlternatingRowBG:         true,
				Model:                    c.model,
				NotSortableByHeaderClick: true,
				Columns: []TableViewColumn{
					{Title: "Item", Width: 300},
					{Title: "Section", Width: 110},
					{Title: "Found", Width: 160, Alignment: AlignFar},
					{Title: "Result", Width: 200},
				},
				StyleCell: func(s *walk.CellStyle) {
					r := c.model.rows[s.Row()]
					if !clean.Selectable(r.t, c.a.elevated) {
						s.TextColor = walk.RGB(150, 150, 150)
					}
				},
				OnCurrentIndexChanged: c.showDescription,
			},
			Label{AssignTo: &c.desc, Text: "Select an item to see what it cleans."},
			ProgressBar{AssignTo: &c.progress, MaxValue: 100},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{AssignTo: &c.status, Text: "Click Analyze to see how much space you can free. Nothing is deleted until you click Run Cleaner."},
					HSpacer{},
					PushButton{AssignTo: &c.openLog, Text: "Open log", Visible: false, OnClicked: func() { sysutil.ShellOpen(c.lastLog) }},
					PushButton{AssignTo: &c.stop, Text: "Stop", Enabled: false, OnClicked: c.cancel},
					PushButton{AssignTo: &c.analyze, Text: "Analyze", MinSize: Size{Width: 110}, OnClicked: func() { c.start(false) }},
					PushButton{AssignTo: &c.run, Text: "Run Cleaner", MinSize: Size{Width: 130}, OnClicked: func() { c.start(true) }},
				},
			},
		},
	}
}

func (c *cleanerTab) init() {
	c.model.onChange = c.saveSelection
	c.model.later = c.a.mw.Synchronize
}

func (c *cleanerTab) showDescription() {
	i := c.table.CurrentIndex()
	if i < 0 || i >= len(c.model.rows) {
		return
	}
	t := c.model.rows[i].t
	text := t.Description
	if !clean.Selectable(t, c.a.elevated) {
		text += "  (Restart CPC as administrator to use this.)"
	}
	c.desc.SetText(text)
}

func (c *cleanerTab) selectWhere(pred func(*clean.Target) bool) {
	if c.busy() {
		return
	}
	for _, r := range c.model.rows {
		r.checked = pred(r.t) && clean.Selectable(r.t, c.a.elevated)
	}
	c.model.PublishRowsReset()
	c.saveSelection()
}

func (c *cleanerTab) saveSelection() {
	for _, r := range c.model.rows {
		if clean.Selectable(r.t, c.a.elevated) {
			c.a.cfg.Selected[r.t.ID] = r.checked
		}
	}
	_ = settings.Save(c.a.dataDir, c.a.cfg)
}

func (c *cleanerTab) busy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cancelFn != nil
}

func (c *cleanerTab) cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancelFn != nil {
		c.cancelFn()
	}
}

func (c *cleanerTab) selected() (rows []int, targets []*clean.Target) {
	for i, r := range c.model.rows {
		if r.checked && clean.Selectable(r.t, c.a.elevated) {
			rows = append(rows, i)
			targets = append(targets, r.t)
		}
	}
	return
}

func (c *cleanerTab) setBusy(b bool) {
	c.analyze.SetEnabled(!b)
	c.run.SetEnabled(!b)
	c.stop.SetEnabled(b)
	c.model.locked = b
}

func (c *cleanerTab) start(doClean bool) {
	if c.busy() {
		return
	}
	rows, targets := c.selected()
	if len(targets) == 0 {
		c.a.warn("Tick at least one item first.")
		return
	}

	if doClean {
		msg := "CPC will permanently delete the selected items. This can't be undone.\n\nContinue?"
		if open := clean.OpenProcesses(targets, sysutil.RunningProcesses()); len(open) > 0 {
			var names []string
			for _, p := range open {
				names = append(names, friendly(p))
			}
			msg = "These programs are open: " + strings.Join(names, ", ") +
				".\n\nClose them first for a full clean. If you continue, files they are using will be skipped.\n\n" + msg
		}
		if walk.MsgBox(c.a.mw, appName, msg, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
			return
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancelFn = cancel
	c.mu.Unlock()

	for _, r := range c.model.rows {
		if doClean {
			r.result = ""
		} else {
			r.found, r.result = "", ""
		}
	}
	c.model.PublishRowsReset()
	c.setBusy(true)
	c.openLog.SetVisible(false)
	c.progress.SetValue(0)

	var log *settings.Log
	if doClean {
		log, _ = settings.NewLog(c.a.dataDir, time.Now())
		log.Printf("%s %s clean started %s", appName, version, time.Now().Format(time.RFC1123))
	}

	go func() {
		var lastUI time.Time
		verb := "Analyzing"
		if doClean {
			verb = "Cleaning"
		}
		var current string
		h := clean.Hooks{
			OnStart: func(i int, t *clean.Target) {
				current = t.Name
				pct := i * 100 / len(targets)
				c.a.mw.Synchronize(func() {
					c.progress.SetValue(pct)
					c.status.SetText(verb + " " + t.Name + "...")
				})
				log.Printf("")
				log.Printf("[%s]", t.Name)
			},
			OnFile: func(p string) {
				if time.Since(lastUI) < 120*time.Millisecond {
					return
				}
				lastUI = time.Now()
				name := current
				c.a.mw.Synchronize(func() { c.status.SetText(verb + " " + name + ": " + shorten(p, 70)) })
			},
			Deleted: func(p string) { log.Printf("deleted %s", p) },
			OnDone: func(i int, t *clean.Target, r clean.Result) {
				row := c.model.rows[rows[i]]
				text := describe(r, doClean)
				c.a.mw.Synchronize(func() {
					if doClean {
						row.result = text
					} else {
						row.found = text
						row.result = ""
						if r.Err != nil {
							row.result = r.Err.Error()
						}
					}
					c.model.PublishRowChanged(rows[i])
				})
				log.Printf("%s", text)
			},
		}
		sum := clean.Run(ctx, c.eng, targets, doClean, h)

		var msg string
		switch {
		case sum.Canceled && doClean:
			msg = fmt.Sprintf("Stopped. Removed %s (%s) before stopping.", clean.FormatBytes(sum.Bytes), clean.Files(sum.Files))
		case sum.Canceled:
			msg = "Analysis stopped."
		case doClean:
			msg = fmt.Sprintf("Done. Freed %s (%s).", clean.FormatBytes(sum.Bytes), clean.Files(sum.Files))
			if sum.Failed > 0 {
				msg += fmt.Sprintf(" %s in use and skipped.", clean.Files(sum.Failed))
			}
		default:
			msg = fmt.Sprintf("Found %s in %s. Click Run Cleaner to remove it.", clean.FormatBytes(sum.Bytes), clean.Files(sum.Files))
		}
		if sum.Errors > 0 {
			msg += fmt.Sprintf(" %d item(s) had errors, see the Result column.", sum.Errors)
		}
		log.Printf("")
		log.Printf("%s", msg)
		logPath := ""
		if log != nil {
			logPath = log.Path
		}
		log.Close()

		c.mu.Lock()
		c.cancelFn = nil
		c.mu.Unlock()
		cancel()

		c.a.mw.Synchronize(func() {
			c.progress.SetValue(100)
			c.status.SetText(msg)
			c.setBusy(false)
			if logPath != "" {
				c.lastLog = logPath
				c.openLog.SetVisible(true)
			}
		})
	}()
}

func describe(r clean.Result, cleaned bool) string {
	if r.Err != nil {
		return "Error: " + r.Err.Error()
	}
	if r.Note != "" && r.Files == 0 {
		return r.Note
	}
	if !cleaned {
		if r.Files == 0 {
			return "Nothing to clean"
		}
		return fmt.Sprintf("%s (%s)", clean.FormatBytes(r.Bytes), clean.Files(r.Files))
	}
	s := fmt.Sprintf("Removed %s (%s)", clean.FormatBytes(r.Bytes), clean.Files(r.Files))
	if r.Files == 0 {
		s = "Nothing removed"
	}
	if r.Failed > 0 {
		s += fmt.Sprintf(", %d in use", r.Failed)
	}
	return s
}

func shorten(p string, n int) string {
	if len(p) <= n {
		return p
	}
	return "..." + p[len(p)-n+3:]
}
