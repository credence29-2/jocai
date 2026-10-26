package bloat

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		group string
		known bool
	}{
		{"king.com.CandyCrushSaga", GPromo, true},
		{"Microsoft.XboxGamingOverlay", GXbox, true},
		{"Microsoft.WindowsCalculator", "", false},
		{"Microsoft.Windows.Photos", "", false},
		{"Microsoft.DesktopAppInstaller", "", false},
		{"SomeVendor.SomeApp", GOther, false},
		{"DellInc.DellSupportAssist", GOEM, true},
	}
	for _, c := range cases {
		a := App{Name: c.name}
		Classify(&a)
		if a.Group != c.group || a.Known != c.known {
			t.Errorf("%s: group=%q known=%v, want %q %v", c.name, a.Group, a.Known, c.group, c.known)
		}
	}
}

func TestPrepareDropsProtectedAndSorts(t *testing.T) {
	out := Prepare([]App{{Name: "Microsoft.WindowsStore"}, {Name: "Vendor.Zed"}, {Name: "Microsoft.BingNews"}, {Name: "Microsoft.XboxApp"}})
	if len(out) != 3 || out[0].Name != "Microsoft.BingNews" || out[1].Name != "Microsoft.XboxApp" || out[2].Name != "Vendor.Zed" {
		t.Fatalf("got %+v", out)
	}
	if !out[0].Checked || out[2].Checked {
		t.Fatal("default ticks wrong")
	}
}

func TestSafeName(t *testing.T) {
	if !SafeName("Microsoft.BingNews_8wekyb3d8bbwe") || SafeName("x; Remove-Item C:\\") || SafeName("") {
		t.Fatal("SafeName wrong")
	}
}
