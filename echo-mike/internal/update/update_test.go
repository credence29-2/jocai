package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"1.3.10", "1.3.9", true}, {"1.3.9", "1.3.10", false}, {"1.3.0", "1.3.0", false}, {"2.0", "1.9.9", true}, {"v1.3.1", "1.3.0", true}, {"", "1.0.0", false}} {
		if Newer(c.a, c.b) != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, !c.want)
		}
	}
}

func TestCheckDownloadApply(t *testing.T) {
	newExe := []byte("new program bytes")
	sum := sha256.Sum256(newExe)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			w.Write([]byte(`{"name":"Project Echo Mike for Windows 1.3.7","body":"1.3.7"}`))
		case "/dl/SHA256SUMS.txt":
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + exeName() + "\n" + "deadbeef  other.exe\n"))
		case "/dl/" + exeName():
			w.Write(newExe)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, "ProjectEchoMike.exe")
	os.WriteFile(exe, []byte("old program"), 0o755)
	u := New("1.3.5", exe)
	u.API, u.Release = srv.URL+"/api", srv.URL+"/dl/"

	info, err := u.Check(context.Background())
	if err != nil || !info.Available || info.Latest != "1.3.7" {
		t.Fatalf("check: %+v %v", info, err)
	}
	var last int64
	if err := u.Download(context.Background(), func(done, total int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	if last != int64(len(newExe)) || !u.Status().Ready {
		t.Fatalf("progress %d ready %v", last, u.Status().Ready)
	}
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new program bytes" {
		t.Fatalf("exe = %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old program" {
		t.Fatal("old program not kept aside")
	}
	Cleanup(exe)
	if _, err := os.Stat(exe + ".old"); err == nil {
		t.Fatal("old copy not cleaned up")
	}

	// A download that doesn't match the checksum is refused.
	u2 := New("1.3.5", exe)
	u2.API, u2.Release = srv.URL+"/api", srv.URL+"/dl/"
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dl/SHA256SUMS.txt" {
			w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  " + exeName() + "\n"))
			return
		}
		w.Write(newExe)
	}))
	defer bad.Close()
	u2.Release = bad.URL + "/dl/"
	if err := u2.Download(context.Background(), nil); err == nil {
		t.Fatal("bad checksum accepted")
	}
	if _, err := os.Stat(exe + ".new"); err == nil {
		t.Fatal("bad download left behind")
	}
}
