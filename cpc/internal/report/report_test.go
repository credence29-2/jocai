package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/credence29-2/jocai/cpc/internal/health"
)

func sample() *Report {
	d := health.Disk{Name: "Samsung SSD", MediaType: "SSD", Size: 512e9, Status: "Healthy", Wear: 80}
	d.Rate()
	b := health.Battery{Present: true, DesignCapacity: 50000, FullCapacity: 20000}
	b.Rate()
	return &Report{
		App: "Kempt", Version: "1.5.0", Company: "804 Industries", Generated: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC), Elevated: true,
		System:  System{Computer: "POS-01", User: "cashier", Maker: "Dell", Model: "OptiPlex", OS: "Windows 11 Pro", Version: "24H2", Build: "26100.1", LastUpdate: "2026-06-01", Antivirus: "None", Activated: "Not activated", RAMBytes: 8 << 30},
		Volumes: []Volume{{Letter: "C:", Label: "OS", Size: 256e9, Free: 10e9}},
		Disks:   []health.Disk{d},
		Battery: b,
		Devices: []Item{{Name: "Unknown USB Device", Detail: "code 43", Flag: "bad"}},
		Startup: []Item{{Name: "OneDrive", Detail: "x"}, {Name: "Spotify", Detail: "y", Flag: "off"}},
	}
}

func TestAssess(t *testing.T) {
	r := sample()
	r.Assess()
	joined := strings.Join(r.Warnings, "\n")
	for _, want := range []string{"96% full", "rated life", "Battery", "device(s)", "90 days", "antivirus", "not activated"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing warning %q in:\n%s", want, joined)
		}
	}
}

func TestTextAndHTML(t *testing.T) {
	r := sample()
	r.Assess()
	txt := r.Text()
	for _, want := range []string{"Kempt tech report", "POS-01", "NEEDS ATTENTION", "C: OS", "Samsung SSD", "STARTUP PROGRAMS (2)", "804 Industries"} {
		if !strings.Contains(txt, want) {
			t.Errorf("text missing %q", want)
		}
	}
	h := r.HTML()
	if !strings.Contains(h, "<title>Kempt tech report, POS-01</title>") || !strings.Contains(h, "Needs attention") || !strings.Contains(h, "Unknown USB Device") {
		t.Error("html missing content")
	}
	r.System.Computer = "<script>"
	if strings.Contains(r.HTML(), "<script>") {
		t.Error("html not escaped")
	}
}

func TestReadCleans(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "clean-20261001-120000.log"), []byte("start\r\n[x]\r\ndeleted a\r\n\r\nFreed 1.2 MB (3 files).\r\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "clean-20261002-120000.log"), []byte("start\r\nFreed 2 MB (1 file).\r\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	c := ReadCleans(dir, 5)
	if len(c) != 2 || !strings.HasPrefix(c[0].Summary, "Freed 2 MB") || !strings.Contains(c[0].When, "Oct 2, 2026") {
		t.Fatalf("got %+v", c)
	}
	if len(ReadCleans(dir, 1)) != 1 {
		t.Fatal("limit ignored")
	}
}
