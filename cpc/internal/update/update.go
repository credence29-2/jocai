// Package update checks GitHub Releases for a newer Kempt, downloads it,
// verifies the SHA-256 and swaps the running exe.
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
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases are published from.
var Repo = "credence29-2/jocai"

// TagPrefix is what release tags start with, e.g. kempt-v1.5.0.
var TagPrefix = "kempt-v"

// Release is a published version.
type Release struct {
	Version  string `json:"version"`
	Tag      string `json:"tag"`
	Notes    string `json:"notes"`
	Page     string `json:"page"`
	Asset    string `json:"asset"`
	AssetURL string `json:"assetUrl"`
	SumsURL  string `json:"sumsUrl"`
	Size     int64  `json:"size"`
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type ghRelease struct {
	Tag        string    `json:"tag_name"`
	Name       string    `json:"name"`
	Body       string    `json:"body"`
	HTMLURL    string    `json:"html_url"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

// AssetName is the exe published for an architecture.
func AssetName(arch string) string {
	if arch == "arm64" {
		return "Kempt-arm64.exe"
	}
	return "Kempt.exe"
}

// VersionFromTag strips the tag prefix: "kempt-v1.5.0" gives "1.5.0".
func VersionFromTag(tag string) string {
	t := strings.TrimSpace(tag)
	for _, p := range []string{TagPrefix, "v", "V"} {
		if strings.HasPrefix(strings.ToLower(t), strings.ToLower(p)) {
			t = t[len(p):]
			break
		}
	}
	return strings.TrimPrefix(t, "v")
}

func parts(v string) []int {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}

// Newer reports whether candidate is a higher version than current.
func Newer(candidate, current string) bool {
	a, b := parts(candidate), parts(current)
	for len(a) < len(b) {
		a = append(a, 0)
	}
	for len(b) < len(a) {
		b = append(b, 0)
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// Pick turns a GitHub release into a Release for this architecture.
func pick(r ghRelease, arch string) (Release, error) {
	rel := Release{Version: VersionFromTag(r.Tag), Tag: r.Tag, Notes: r.Body, Page: r.HTMLURL}
	want := AssetName(arch)
	for _, a := range r.Assets {
		switch strings.ToLower(a.Name) {
		case strings.ToLower(want):
			rel.Asset, rel.AssetURL, rel.Size = a.Name, a.URL, a.Size
		case "sha256sums.txt":
			rel.SumsURL = a.URL
		}
	}
	if rel.AssetURL == "" {
		return rel, fmt.Errorf("the release has no %s", want)
	}
	return rel, nil
}

// ParseSums reads a SHA256SUMS.txt file into name to hex digest.
func ParseSums(text string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 || len(f[0]) != 64 {
			continue
		}
		name := strings.TrimPrefix(f[len(f)-1], "*")
		out[strings.ToLower(filepath.Base(strings.ReplaceAll(name, `\`, "/")))] = strings.ToLower(f[0])
	}
	return out
}

var client = &http.Client{Timeout: 10 * time.Minute}

func get(ctx context.Context, url, ua string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream;q=0.9, */*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		if resp.StatusCode == 404 {
			return nil, errors.New("no release has been published yet")
		}
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	return resp, nil
}

// Check asks GitHub for the latest release. It returns the release and
// whether it is newer than current.
func Check(ctx context.Context, current string) (Release, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := get(ctx, "https://api.github.com/repos/"+Repo+"/releases/latest", "Kempt/"+current)
	if err != nil {
		return Release{}, false, err
	}
	defer resp.Body.Close()
	var r ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return Release{}, false, errors.New("could not read the release list")
	}
	if !strings.HasPrefix(strings.ToLower(r.Tag), strings.ToLower(TagPrefix)) {
		return Release{}, false, errors.New("no Kempt release has been published yet")
	}
	rel, err := pick(r, runtime.GOARCH)
	if err != nil {
		return rel, false, err
	}
	return rel, Newer(rel.Version, current), nil
}

// Progress reports download progress.
type Progress func(done, total int64)

// Download fetches the release exe into dir and checks it against
// SHA256SUMS.txt. It returns the downloaded file's path.
func Download(ctx context.Context, rel Release, dir string, prog Progress) (string, error) {
	if rel.SumsURL == "" {
		return "", errors.New("the release has no SHA256SUMS.txt, so the download can't be verified")
	}
	ua := "Kempt-updater"
	resp, err := get(ctx, rel.SumsURL, ua)
	if err != nil {
		return "", err
	}
	sumText, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	want := ParseSums(string(sumText))[strings.ToLower(rel.Asset)]
	if want == "" {
		return "", errors.New("SHA256SUMS.txt has no entry for " + rel.Asset)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "Kempt-"+rel.Version+".exe.part")
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	resp, err = get(ctx, rel.AssetURL, ua)
	if err != nil {
		f.Close()
		os.Remove(dest)
		return "", err
	}
	defer resp.Body.Close()
	h := sha256.New()
	var done int64
	buf := make([]byte, 256<<10)
	total := resp.ContentLength
	if total <= 0 {
		total = rel.Size
	}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(dest)
				return "", werr
			}
			h.Write(buf[:n])
			done += int64(n)
			if prog != nil {
				prog(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(dest)
			return "", rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(dest)
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(dest)
		return "", errors.New("the download did not match its published checksum, so it was discarded")
	}
	final := strings.TrimSuffix(dest, ".part")
	os.Remove(final)
	if err := os.Rename(dest, final); err != nil {
		return "", err
	}
	return final, nil
}

// OldSuffix is appended to the previous exe during a swap.
const OldSuffix = ".old"

// Apply swaps exe for newFile: the running exe is renamed aside (Windows
// allows that) and the new file takes its place. On failure the old exe is
// put back.
func Apply(exe, newFile string) error {
	old := exe + OldSuffix
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("could not replace %s: %v. Kempt may be in a folder that needs administrator rights", filepath.Base(exe), err)
	}
	if err := os.Rename(newFile, exe); err != nil {
		if err2 := copyFile(newFile, exe); err2 != nil {
			_ = os.Rename(old, exe)
			return fmt.Errorf("could not put the new version in place: %v", err)
		}
		os.Remove(newFile)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Relaunch starts exe with args and returns once it is running.
func Relaunch(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	return cmd.Start()
}

// CleanupOld removes the exe left aside by a previous Apply. Call it at
// startup; it is harmless when there is nothing to remove.
func CleanupOld(exe string) {
	old := exe + OldSuffix
	for i := 0; i < 5; i++ {
		if err := os.Remove(old); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}
