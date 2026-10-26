package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{{"1.5.0", "1.4.2", true}, {"1.4.2", "1.4.2", false}, {"1.4.10", "1.4.9", true}, {"1.4", "1.4.0", false}, {"2.0.0", "1.99.99", true}, {"1.4.2", "1.5.0", false}, {"1.5.0-beta", "1.4.2", true}, {"1.4.3", "1.4.3.1", false}}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestVersionFromTag(t *testing.T) {
	for in, want := range map[string]string{"kempt-v1.5.0": "1.5.0", "v1.5.0": "1.5.0", "1.5.0": "1.5.0", "Kempt-v2.0": "2.0"} {
		if got := VersionFromTag(in); got != want {
			t.Errorf("VersionFromTag(%q) = %q", in, got)
		}
	}
}

func TestPickAndSums(t *testing.T) {
	r := ghRelease{Tag: "kempt-v1.5.0", HTMLURL: "https://x", Assets: []ghAsset{{Name: "Kempt.exe", URL: "u1", Size: 10}, {Name: "Kempt-arm64.exe", URL: "u2"}, {Name: "SHA256SUMS.txt", URL: "u3"}}}
	rel, err := pick(r, "amd64")
	if err != nil || rel.AssetURL != "u1" || rel.SumsURL != "u3" || rel.Version != "1.5.0" {
		t.Fatalf("pick amd64: %+v %v", rel, err)
	}
	rel, _ = pick(r, "arm64")
	if rel.AssetURL != "u2" {
		t.Fatalf("pick arm64: %+v", rel)
	}
	if _, err := pick(ghRelease{Tag: "kempt-v1"}, "amd64"); err == nil {
		t.Fatal("release without exe accepted")
	}
	sums := ParseSums("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789  Kempt.exe\n0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef *dist/Kempt-arm64.exe\nbad line\n")
	if sums["kempt.exe"] == "" || sums["kempt-arm64.exe"] == "" || len(sums) != 2 {
		t.Fatalf("sums: %v", sums)
	}
}

func TestApplyAndCleanup(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "Kempt.exe")
	nw := filepath.Join(dir, "Kempt-1.5.0.exe")
	os.WriteFile(exe, []byte("old"), 0o755)
	os.WriteFile(nw, []byte("new"), 0o755)
	if err := Apply(exe, nw); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatal("exe not replaced")
	}
	if b, _ := os.ReadFile(exe + OldSuffix); string(b) != "old" {
		t.Fatal("old exe not kept aside")
	}
	CleanupOld(exe)
	if _, err := os.Stat(exe + OldSuffix); !os.IsNotExist(err) {
		t.Fatal("old exe not removed")
	}
	if err := Apply(filepath.Join(dir, "missing.exe"), nw); err == nil {
		t.Fatal("apply with missing exe succeeded")
	}
}
