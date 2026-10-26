// Package bloat lists preinstalled Store apps that most people never use and
// removes the ones chosen.
package bloat

import (
	"sort"
	"strings"
)

// App is one installed Store app.
type App struct {
	Name      string `json:"name"`  // package family or full name, as PowerShell reports it
	Label     string `json:"label"` // friendly name
	Publisher string `json:"publisher"`
	Version   string `json:"version"`
	Size      int64  `json:"size"`
	Group     string `json:"group"`
	Known     bool   `json:"known"` // on the bloat list
	Note      string `json:"note"`
	Checked   bool   `json:"checked"`
}

// Groups in display order.
const (
	GPromo  = "Promotions and games"
	GExtras = "Microsoft extras"
	GXbox   = "Xbox"
	GOEM    = "PC maker apps"
	GOther  = "Other Store apps"
)

var GroupOrder = []string{GPromo, GExtras, GXbox, GOEM, GOther}

type known struct {
	match   string // lowercase prefix of the package name
	label   string
	group   string
	note    string
	checked bool
}

// The bloat list. Prefix match on the package name, lower case.
var knownApps = []known{
	{"king.com.", "King games (Candy Crush and others)", GPromo, "", true},
	{"microsoft.bingnews", "Microsoft News", GPromo, "", true},
	{"microsoft.bingweather", "Weather", GPromo, "", false},
	{"microsoft.bingfinance", "Money", GPromo, "", true},
	{"microsoft.bingsports", "Sports", GPromo, "", true},
	{"microsoft.microsoftsolitairecollection", "Solitaire Collection", GPromo, "", true},
	{"microsoft.mixedreality.portal", "Mixed Reality Portal", GPromo, "", true},
	{"microsoft.skypeapp", "Skype", GPromo, "", true},
	{"microsoft.getstarted", "Tips", GExtras, "", true},
	{"microsoft.gethelp", "Get Help", GExtras, "Microsoft's support app. Harmless, rarely used.", false},
	{"microsoft.people", "People", GExtras, "", true},
	{"microsoft.windowsfeedbackhub", "Feedback Hub", GExtras, "", true},
	{"microsoft.windowsmaps", "Maps", GExtras, "", true},
	{"microsoft.zunemusic", "Groove Music / Media Player", GExtras, "Also the default music player on older Windows 10.", false},
	{"microsoft.zunevideo", "Movies & TV", GExtras, "", false},
	{"microsoft.microsoftofficehub", "Office hub", GExtras, "Shortcut to Office online. Not Office itself.", true},
	{"microsoft.onenote", "OneNote (Store version)", GExtras, "The desktop OneNote in Office is separate.", false},
	{"microsoft.yourphone", "Phone Link", GExtras, "", false},
	{"microsoft.3dbuilder", "3D Builder", GExtras, "", true},
	{"microsoft.microsoft3dviewer", "3D Viewer", GExtras, "", true},
	{"microsoft.print3d", "Print 3D", GExtras, "", true},
	{"microsoft.wallet", "Wallet", GExtras, "", true},
	{"microsoft.messaging", "Messaging", GExtras, "", true},
	{"microsoft.oneconnect", "Mobile Plans", GExtras, "", true},
	{"microsoft.todos", "To Do", GExtras, "", false},
	{"microsoft.windowscommunicationsapps", "Mail and Calendar", GExtras, "Keep if anyone uses the built-in Mail app.", false},
	{"microsoft.powerautomatedesktop", "Power Automate", GExtras, "", true},
	{"microsoft.549981c3f5f10", "Cortana", GExtras, "", true},
	{"clipchamp.clipchamp", "Clipchamp", GExtras, "", true},
	{"microsoftteams", "Teams (personal)", GExtras, "The consumer Teams chat, not Teams for work.", true},
	{"microsoft.xboxapp", "Xbox Console Companion", GXbox, "", true},
	{"microsoft.xbox.tcui", "Xbox TCUI", GXbox, "", true},
	{"microsoft.xboxgameoverlay", "Xbox Game Overlay", GXbox, "", true},
	{"microsoft.xboxgamingoverlay", "Xbox Game Bar", GXbox, "", true},
	{"microsoft.xboxidentityprovider", "Xbox Identity Provider", GXbox, "Needed to sign in to Xbox services. Remove only if nobody games on this PC.", false},
	{"microsoft.xboxspeechtotextoverlay", "Xbox Speech to Text", GXbox, "", true},
	{"microsoft.gamingapp", "Xbox app", GXbox, "", true},
	{"spotifyab.", "Spotify (preinstalled)", GPromo, "", true},
	{"disney.", "Disney+", GPromo, "", true},
	{"facebook.", "Facebook / Instagram", GPromo, "", true},
	{"bytedance.tiktok", "TikTok", GPromo, "", true},
	{"amazon.com.amazon", "Amazon", GPromo, "", true},
	{"netflix.", "Netflix", GPromo, "", true},
	{"hulu", "Hulu", GPromo, "", true},
	{"booking.com", "Booking.com", GPromo, "", true},
	{"adobesystemsincorporated.adobephotoshopexpress", "Adobe Photoshop Express", GPromo, "", true},
	{"dolby", "Dolby", GOEM, "", false},
	{"hpinc.", "HP preinstalled apps", GOEM, "Some HP support tools live here. Review before removing.", false},
	{"ad2f1837.", "HP preinstalled apps", GOEM, "Some HP support tools live here. Review before removing.", false},
	{"dellinc.", "Dell preinstalled apps", GOEM, "Dell support tools. Review before removing.", false},
	{"lenovo", "Lenovo preinstalled apps", GOEM, "Lenovo Vantage and friends. Review before removing.", false},
	{"e046963f.", "Lenovo preinstalled apps", GOEM, "", false},
	{"asus", "ASUS preinstalled apps", GOEM, "", false},
	{"b9ecED6f.", "ASUS preinstalled apps", GOEM, "", false},
	{"acer", "Acer preinstalled apps", GOEM, "", false},
	{"mcafee", "McAfee trial", GOEM, "Trial antivirus. Remove if a different antivirus is in use.", false},
	{"nortonlifelock", "Norton trial", GOEM, "Trial antivirus. Remove if a different antivirus is in use.", false},
}

// Never offered for removal: things Windows needs or that are not apps.
var protectedPrefixes = []string{
	"microsoft.windows.", "microsoft.windowsstore", "microsoft.storepurchaseapp", "microsoft.desktopappinstaller",
	"microsoft.vclibs", "microsoft.net.", "microsoft.ui.xaml", "microsoft.services.store", "microsoft.windowsterminal",
	"microsoft.windowscalculator", "microsoft.windowsnotepad", "microsoft.paint", "microsoft.mspaint", "microsoft.screensketch",
	"microsoft.windowscamera", "microsoft.windowsalarms", "microsoft.windowssoundrecorder", "microsoft.photos", "microsoft.windows.photos",
	"microsoft.sechealthui", "microsoft.windowssecurity", "microsoft.heifimageextension", "microsoft.hevcvideoextension",
	"microsoft.vp9videoextensions", "microsoft.webmediaextensions", "microsoft.webpimageextension", "microsoft.rawimageextension",
	"microsoft.avcencodervideoextension", "microsoft.mpeg2videoextension", "microsoft.aacdecoderextension",
	"microsoft.winappruntime", "microsoft.windowsappruntime", "microsoft.edge", "microsoft.microsoftedge", "microsoft.webview2",
	"microsoft.windowsterminal", "microsoft.powershell", "microsoft.winget", "microsoft.xboxgamecallableui", "microsoft.lockapp",
	"microsoft.accountscontrol", "microsoft.aad.", "microsoft.async", "microsoft.bioenrollment", "microsoft.credd", "microsoft.ecapp",
	"microsoft.win32webviewhost", "microsoft.windows.shell", "microsoft.windows.search", "microsoft.windows.startmenu",
	"microsoft.windows.cloudexperiencehost", "microsoft.windows.contentdeliverymanager", "microsoft.windows.oobe",
	"microsoft.windows.peopleexperiencehost", "microsoft.windows.secureassessmentbrowser", "microsoft.windows.apprep",
	"microsoft.windows.narratorquickstart", "microsoft.windows.parentalcontrols", "microsoft.windows.xgpueject",
	"microsoft.microsoftstickynotes", "microsoft.windowsmaps.", "microsoft.outlookforwindows", "microsoft.copilot",
	"microsoftwindows.client", "microsoftwindows.", "windows.", "nvidiacorp.nvidiacontrolpanel", "realtek", "intel", "amd",
	"synaptics", "elan", "waves", "conexant", "cirrus", "microsoft.directx", "microsoft.advertising",
}

// Classify decides whether a package is offered and under which group.
func Classify(a *App) {
	n := strings.ToLower(a.Name)
	for _, p := range protectedPrefixes {
		if strings.HasPrefix(n, p) {
			a.Group = ""
			return
		}
	}
	for _, k := range knownApps {
		if strings.HasPrefix(n, k.match) {
			a.Known, a.Group, a.Note, a.Checked = true, k.group, k.note, k.checked
			if a.Label == "" || a.Label == a.Name {
				a.Label = k.label
			}
			return
		}
	}
	a.Group = GOther
	a.Checked = false
	if a.Label == "" {
		a.Label = a.Name
	}
}

// Prepare classifies and sorts a list, dropping protected packages.
func Prepare(in []App) []App {
	var out []App
	for _, a := range in {
		Classify(&a)
		if a.Group == "" {
			continue
		}
		out = append(out, a)
	}
	order := map[string]int{}
	for i, g := range GroupOrder {
		order[g] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		if order[out[i].Group] != order[out[j].Group] {
			return order[out[i].Group] < order[out[j].Group]
		}
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
	})
	return out
}

// SafeName reports whether a package name is one PowerShell can be handed
// without any quoting tricks.
func SafeName(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
