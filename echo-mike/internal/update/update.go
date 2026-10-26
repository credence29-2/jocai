// Package update checks GitHub for a newer Windows build, downloads it,
// checks it against the published checksums, and swaps the running exe.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Release is where the published builds live.
const Release = "https://github.com/credence29-2/jocai/releases/download/echo-mike-windows/"

// API is the release's metadata; the title carries the version.
const API = "https://api.github.com/repos/credence29-2/jocai/releases/tags/echo-mike-windows"

// HTTPClient is used for the checks and downloads. Tests replace it.
var HTTPClient = &http.Client{Timeout: 5 * time.Minute}

// Info is what the check found.
type Info struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest"`
	Available bool      `json:"available"`
	CheckedAt time.Time `json:"checkedAt"`
	// Ready is set once the new exe is downloaded and verified; the app
	// swaps it in on restart or when asked.
	Ready bool   `json:"ready"`
	Error string `json:"error,omitempty"`
}

// Updater checks and applies updates for one installed exe.
type Updater struct {
	Current string
	// Exe is the running program's path.
	Exe string
	// API and Release override the GitHub locations in tests.
	API, Release string

	mu    sync.Mutex
	info  Info
	ready string // path of the verified new exe, next to the current one
}

func New(current, exe string) *Updater {
	return &Updater{Current: current, Exe: exe, API: API, Release: Release, info: Info{Current: current}}
}

// Status returns the last check's result.
func (u *Updater) Status() Info {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.info
}

// Check asks GitHub for the latest version.
func (u *Updater) Check(ctx context.Context) (Info, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", u.API, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ProjectEchoMike/"+u.Current)
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return u.fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return u.fail(fmt.Errorf("GitHub answered %s", resp.Status))
	}
	var rel struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return u.fail(err)
	}
	latest := strings.TrimSpace(rel.Body)
	if latest == "" {
		if i := strings.LastIndex(rel.Name, " "); i >= 0 {
			latest = rel.Name[i+1:]
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.info = Info{Current: u.Current, Latest: latest, Available: Newer(latest, u.Current), CheckedAt: time.Now(), Ready: u.ready != ""}
	return u.info, nil
}

func (u *Updater) fail(err error) (Info, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.info.Error = err.Error()
	u.info.CheckedAt = time.Now()
	return u.info, err
}

// Newer reports whether version a is newer than b, comparing dotted numbers.
func Newer(a, b string) bool {
	pa, pb := parts(a), parts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func parts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		out = append(out, n)
	}
	return out
}

// exeName is the published file for this machine.
func exeName() string {
	if runtime.GOARCH == "arm64" {
		return "ProjectEchoMike-arm64.exe"
	}
	return "ProjectEchoMike.exe"
}

// Download fetches the new exe next to the current one and verifies it
// against SHA256SUMS.txt. It reports progress as bytes done and total.
func (u *Updater) Download(ctx context.Context, progress func(done, total int64)) error {
	sums, err := u.fetch(ctx, u.Release+"SHA256SUMS.txt", nil)
	if err != nil {
		return fmt.Errorf("couldn't read the checksums: %w", err)
	}
	want := ""
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == exeName() {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return errors.New("the published checksums don't list " + exeName())
	}
	tmp := u.Exe + ".new"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = u.fetch(ctx, u.Release+exeName(), func(r io.Reader, total int64) error {
		var done int64
		buf := make([]byte, 256<<10)
		for {
			n, rerr := r.Read(buf)
			if n > 0 {
				if _, werr := f.Write(buf[:n]); werr != nil {
					return werr
				}
				h.Write(buf[:n])
				done += int64(n)
				if progress != nil {
					progress(done, total)
				}
			}
			if rerr == io.EOF {
				return nil
			}
			if rerr != nil {
				return rerr
			}
		}
	})
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(tmp)
		return errors.New("the download didn't match its checksum, so it was thrown away")
	}
	u.mu.Lock()
	u.ready = tmp
	u.info.Ready = true
	u.mu.Unlock()
	return nil
}

// fetch gets a URL. With a body callback it streams; without, it returns
// the bytes.
func (u *Updater) fetch(ctx context.Context, url string, body func(io.Reader, int64) error) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "ProjectEchoMike/"+u.Current)
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", filepath.Base(url), resp.Status)
	}
	if body != nil {
		return nil, body(resp.Body, resp.ContentLength)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// Apply swaps the verified exe in: the running one is renamed aside (Windows
// allows that for a running program) and the new one takes its path. The
// caller then restarts. The old copy is removed on the next start.
func (u *Updater) Apply() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.ready == "" {
		return errors.New("no update is downloaded")
	}
	old := u.Exe + ".old"
	os.Remove(old)
	if err := os.Rename(u.Exe, old); err != nil {
		return fmt.Errorf("couldn't move the current program aside: %w", err)
	}
	if err := os.Rename(u.ready, u.Exe); err != nil {
		// Put things back so the app still starts.
		os.Rename(old, u.Exe)
		return fmt.Errorf("couldn't put the new program in place: %w", err)
	}
	u.ready = ""
	u.info.Ready = false
	return nil
}

// Cleanup removes leftovers from a previous update.
func Cleanup(exe string) {
	os.Remove(exe + ".old")
	os.Remove(exe + ".new")
}
