package repair

import (
	"strings"
	"testing"
)

func TestDecodeUTF16(t *testing.T) {
	src := "Verification 42% complete."
	var b []byte
	b = append(b, 0xFF, 0xFE)
	for _, r := range src {
		b = append(b, byte(r), 0)
	}
	if got := Decode(b); got != src {
		t.Fatalf("got %q", got)
	}
	if got := Decode([]byte("plain text")); got != "plain text" {
		t.Fatalf("plain = %q", got)
	}
}

func TestLines(t *testing.T) {
	raw := "Beginning system scan.\r\n\r\nVerification 10% complete.\rVerification 55% complete.\r\nWindows Resource Protection did not find any integrity violations.\r\n"
	lines, pct := Lines(raw)
	if pct != 55 {
		t.Fatalf("pct = %d", pct)
	}
	if len(lines) != 3 || lines[1] != "Verification 55% complete." {
		t.Fatalf("lines = %q", lines)
	}
	if _, p := Lines("no numbers here"); p != -1 {
		t.Fatal("pct should be -1")
	}
}

func TestSummary(t *testing.T) {
	ok, text := Summary("sfc", "Windows Resource Protection did not find any integrity violations.", true)
	if !ok || !strings.Contains(text, "No damaged") {
		t.Fatalf("sfc clean: %v %q", ok, text)
	}
	ok, _ = Summary("sfc", "Windows Resource Protection found corrupt files but was unable to fix some of them.", true)
	if ok {
		t.Fatal("unable to fix should be not ok")
	}
	ok, _ = Summary("dism", "Error: 0x800f081f The source files could not be found.", false)
	if ok {
		t.Fatal("dism source error should be not ok")
	}
	if ok, _ := Summary("network", "", true); !ok {
		t.Fatal("network ok")
	}
	if ok, _ := Summary("unknown", "", false); ok {
		t.Fatal("unknown failing should be not ok")
	}
}

func TestToolsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range Tools {
		if tl.ID == "" || tl.Name == "" || tl.Description == "" || tl.When == "" || len(tl.Steps) == 0 || seen[tl.ID] {
			t.Errorf("bad tool %+v", tl)
		}
		seen[tl.ID] = true
		if _, ok := ByID(tl.ID); !ok {
			t.Errorf("ByID(%s) missing", tl.ID)
		}
	}
}
