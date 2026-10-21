package main

import (
	"strconv"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/credence29-2/jocai/cpc/internal/clean"
	"github.com/credence29-2/jocai/cpc/internal/programs"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

func itoa(n int) string { return strconv.Itoa(n) }

type programsModel struct {
	walk.TableModelBase
	all  []programs.Program
	rows []programs.Program
}

func (m *programsModel) RowCount() int { return len(m.rows) }

func (m *programsModel) Value(row, col int) interface{} {
	p := m.rows[row]
	switch col {
	case 0:
		return p.Name
	case 1:
		return p.Publisher
	case 2:
		return p.Version
	case 3:
		if p.SizeKB <= 0 {
			return ""
		}
		return clean.FormatBytes(p.SizeKB * 1024)
	case 4:
		if p.InstallDate.IsZero() {
			return ""
		}
		return p.InstallDate.Format("2006-01-02")
	}
	return ""
}

func (m *programsModel) filter(q string) {
	q = strings.ToLower(strings.TrimSpace(q))
	m.rows = m.rows[:0]
	for _, p := range m.all {
		if q == "" || strings.Contains(strings.ToLower(p.Name), q) || strings.Contains(strings.ToLower(p.Publisher), q) {
			m.rows = append(m.rows, p)
		}
	}
	m.PublishRowsReset()
}

type programsTab struct {
	a      *app
	model  *programsModel
	table  *walk.TableView
	search *walk.LineEdit
	status *walk.Label
}

func newProgramsTab(a *app) *programsTab {
	return &programsTab{a: a, model: &programsModel{}}
}

func (p *programsTab) page() TabPage {
	return TabPage{
		Title:  "Programs",
		Layout: VBox{},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{Text: "Search:"},
					LineEdit{AssignTo: &p.search, OnTextChanged: func() { p.model.filter(p.search.Text()); p.updateStatus() }},
				},
			},
			TableView{
				AssignTo:                 &p.table,
				Model:                    p.model,
				AlternatingRowBG:         true,
				NotSortableByHeaderClick: true,
				Columns: []TableViewColumn{
					{Title: "Name", Width: 340},
					{Title: "Publisher", Width: 220},
					{Title: "Version", Width: 120},
					{Title: "Size", Width: 90, Alignment: AlignFar},
					{Title: "Installed"},
				},
				OnItemActivated: p.uninstall,
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{AssignTo: &p.status},
					HSpacer{},
					PushButton{Text: "Open install folder", OnClicked: p.openFolder},
					PushButton{Text: "Refresh", OnClicked: p.refresh},
					PushButton{Text: "Uninstall...", OnClicked: p.uninstall},
				},
			},
		},
	}
}

func (p *programsTab) refresh() {
	p.model.all = programs.List()
	p.model.rows = make([]programs.Program, 0, len(p.model.all))
	p.model.filter(p.search.Text())
	p.updateStatus()
}

func (p *programsTab) updateStatus() {
	p.status.SetText(itoa(len(p.model.rows)) + " of " + itoa(len(p.model.all)) + " programs")
}

func (p *programsTab) current() (programs.Program, bool) {
	i := p.table.CurrentIndex()
	if i < 0 || i >= len(p.model.rows) {
		return programs.Program{}, false
	}
	return p.model.rows[i], true
}

func (p *programsTab) uninstall() {
	prog, ok := p.current()
	if !ok {
		p.a.warn("Pick a program first.")
		return
	}
	msg := "Run the uninstaller for \"" + prog.Name + "\"?\n\nThe program's own uninstaller will open and walk you through it."
	if walk.MsgBox(p.a.mw, appName, msg, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	if err := programs.RunUninstaller(prog); err != nil {
		p.a.warn("Could not start the uninstaller: " + err.Error())
		return
	}
	p.status.SetText("Uninstaller started for " + prog.Name + ". Click Refresh when it finishes.")
}

func (p *programsTab) openFolder() {
	prog, ok := p.current()
	if !ok {
		return
	}
	if prog.Location == "" {
		p.a.warn("\"" + prog.Name + "\" did not record an install folder.")
		return
	}
	if err := sysutil.ShellOpen(prog.Location); err != nil {
		p.a.warn("Could not open the folder: " + err.Error())
	}
}
