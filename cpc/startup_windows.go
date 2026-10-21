package main

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/credence29-2/jocai/cpc/internal/startup"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

type startupModel struct {
	walk.TableModelBase
	rows []startup.Entry
}

func (m *startupModel) RowCount() int { return len(m.rows) }

func (m *startupModel) Value(row, col int) interface{} {
	e := m.rows[row]
	switch col {
	case 0:
		if e.Enabled {
			return "Enabled"
		}
		return "Disabled"
	case 1:
		return e.Name
	case 2:
		return e.Location
	case 3:
		return e.Command
	}
	return ""
}

type startupTab struct {
	a       *app
	model   *startupModel
	table   *walk.TableView
	status  *walk.Label
	enable  *walk.PushButton
	disable *walk.PushButton
}

func newStartupTab(a *app) *startupTab {
	return &startupTab{a: a, model: &startupModel{}}
}

func (s *startupTab) page() TabPage {
	return TabPage{
		Title:  "Startup",
		Layout: VBox{},
		Children: []Widget{
			Label{Text: "Programs that start when you sign in. Disabling keeps the entry so you can turn it back on later. Task Manager sees the same setting."},
			TableView{
				AssignTo:         &s.table,
				Model:            s.model,
				AlternatingRowBG: true,
				Columns: []TableViewColumn{
					{Title: "Status", Width: 80},
					{Title: "Name", Width: 220},
					{Title: "Location", Width: 190},
					{Title: "Command", Width: 420},
				},
				StyleCell: func(st *walk.CellStyle) {
					if st.Row() < len(s.model.rows) && !s.model.rows[st.Row()].Enabled {
						st.TextColor = walk.RGB(150, 150, 150)
					}
				},
				OnCurrentIndexChanged: s.updateButtons,
				OnItemActivated:       s.toggle,
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{AssignTo: &s.status},
					HSpacer{},
					PushButton{Text: "Open file location", OnClicked: s.openLocation},
					PushButton{Text: "Refresh", OnClicked: s.refresh},
					PushButton{AssignTo: &s.enable, Text: "Enable", Enabled: false, OnClicked: func() { s.set(true) }},
					PushButton{AssignTo: &s.disable, Text: "Disable", Enabled: false, OnClicked: func() { s.set(false) }},
				},
			},
		},
	}
}

func (s *startupTab) refresh() {
	cur := s.table.CurrentIndex()
	s.model.rows = startup.List()
	s.model.PublishRowsReset()
	if cur >= 0 && cur < len(s.model.rows) {
		s.table.SetCurrentIndex(cur)
	}
	on := 0
	for _, e := range s.model.rows {
		if e.Enabled {
			on++
		}
	}
	s.status.SetText(itoa(len(s.model.rows)) + " startup items, " + itoa(on) + " enabled.")
	s.updateButtons()
}

func (s *startupTab) current() (startup.Entry, bool) {
	i := s.table.CurrentIndex()
	if i < 0 || i >= len(s.model.rows) {
		return startup.Entry{}, false
	}
	return s.model.rows[i], true
}

func (s *startupTab) updateButtons() {
	e, ok := s.current()
	s.enable.SetEnabled(ok && !e.Enabled)
	s.disable.SetEnabled(ok && e.Enabled)
}

func (s *startupTab) toggle() {
	if e, ok := s.current(); ok {
		s.set(!e.Enabled)
	}
}

func (s *startupTab) set(enabled bool) {
	e, ok := s.current()
	if !ok {
		return
	}
	if e.NeedsAdmin && !s.a.elevated {
		s.a.warn("\"" + e.Name + "\" applies to all users. Restart CPC as administrator to change it.")
		return
	}
	if err := startup.SetEnabled(e, enabled); err != nil {
		s.a.warn("Could not change \"" + e.Name + "\": " + err.Error())
	}
	s.refresh()
}

func (s *startupTab) openLocation() {
	if e, ok := s.current(); ok {
		if err := sysutil.ShowInExplorer(startup.Target(e)); err != nil {
			s.a.warn("Could not open the location: " + err.Error())
		}
	}
}
