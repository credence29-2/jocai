// Command cpc is Credence PC Cleaner, a small CCleaner replacement for Windows.
package main

import (
	"os"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/credence29-2/jocai/cpc/internal/settings"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "1.0.0"

const appName = "Credence PC Cleaner"

type app struct {
	mw       *walk.MainWindow
	dataDir  string
	elevated bool
	cfg      settings.Settings

	cleaner  *cleanerTab
	startup  *startupTab
	programs *programsTab
}

func main() {
	for _, a := range os.Args[1:] {
		switch strings.ToLower(a) {
		case "/auto", "-auto", "--auto":
			os.Exit(runAuto())
		}
	}

	a := &app{dataDir: settings.Dir(), elevated: sysutil.IsElevated()}
	a.cfg = settings.Load(a.dataDir)
	a.cleaner = newCleanerTab(a)
	a.startup = newStartupTab(a)
	a.programs = newProgramsTab(a)

	var icon interface{}
	if ic, err := walk.NewIconFromResource("APP"); err == nil {
		icon = ic
	}

	adminText := "Running as administrator"
	if !a.elevated {
		adminText = "Not running as administrator. Some items are unavailable."
	}

	var adminBtn *walk.PushButton
	err := MainWindow{
		AssignTo: &a.mw,
		Title:    appName,
		Icon:     icon,
		Size:     Size{Width: 1000, Height: 680},
		MinSize:  Size{Width: 760, Height: 500},
		Layout:   VBox{Margins: Margins{Left: 8, Top: 8, Right: 8, Bottom: 4}},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{Text: appName, Font: Font{PointSize: 14, Bold: true}},
					Label{Text: "v" + version, TextColor: walk.RGB(110, 110, 110)},
					HSpacer{},
					Label{Text: adminText, TextColor: walk.RGB(110, 110, 110)},
					PushButton{
						AssignTo:  &adminBtn,
						Text:      "Restart as administrator",
						Visible:   !a.elevated,
						OnClicked: a.restartElevated,
					},
				},
			},
			TabWidget{
				Pages: []TabPage{
					a.cleaner.page(),
					a.startup.page(),
					a.programs.page(),
				},
			},
		},
	}.Create()
	if err != nil {
		walk.MsgBox(nil, appName, "Could not start: "+err.Error(), walk.MsgBoxIconError)
		return
	}

	// Stretch the last column to fill the table. Done after creation because
	// walk cannot size columns that do not exist yet. Purely cosmetic.
	for _, tv := range []*walk.TableView{a.cleaner.table, a.startup.table, a.programs.table} {
		_ = tv.SetLastColumnStretched(true)
	}

	a.cleaner.init()
	a.startup.refresh()
	a.programs.refresh()

	a.mw.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		if a.cleaner.busy() {
			a.cleaner.cancel()
		}
		a.cleaner.saveSelection()
	})
	a.mw.Run()
}

func (a *app) restartElevated() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if err := sysutil.ShellRunAs(exe); err != nil {
		// The user said no to the UAC prompt. Stay open.
		return
	}
	a.mw.Close()
}

func (a *app) warn(msg string) {
	walk.MsgBox(a.mw, appName, msg, walk.MsgBoxIconWarning)
}
