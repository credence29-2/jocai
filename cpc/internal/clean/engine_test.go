package clean

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func write(t *testing.T, p string, size int, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestSafeRoot(t *testing.T) {
	bad := []string{"", "%TEMP%", `relative\dir`, "/", "/tmp"}
	good := []string{"/tmp/x", "/home/user/cache"}
	if runtime.GOOS == "windows" {
		bad = append(bad, `C:\`, `C:\Windows`, `C:`, `\Temp`, `%LOCALAPPDATA%\Temp`)
		good = []string{`C:\Windows\Temp`, `C:\Users\Bob\AppData`}
	}
	for _, p := range bad {
		if SafeRoot(p) {
			t.Errorf("SafeRoot(%q) = true, want false", p)
		}
	}
	for _, p := range good {
		if !SafeRoot(p) {
			t.Errorf("SafeRoot(%q) = false, want true", p)
		}
	}
}

func TestExpandEnvLeavesUnknown(t *testing.T) {
	t.Setenv("CPC_TEST_DIR", "/a/b")
	if got := ExpandEnv(`%CPC_TEST_DIR%/c`); got != "/a/b/c" {
		t.Errorf("got %q", got)
	}
	if got := ExpandEnv(`%CPC_NOPE_XYZ%/c`); got != `%CPC_NOPE_XYZ%/c` {
		t.Errorf("unknown var should stay, got %q", got)
	}
}

func TestUnresolvedVariableNeverCleans(t *testing.T) {
	e := NewEngine()
	tg := &Target{Rules: []Rule{{Root: `%CPC_DEFINITELY_UNSET%/Temp`, Recursive: true}}}
	res := e.Clean(context.Background(), tg, nil, nil)
	if res.Files != 0 || res.Err != nil {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestScanAndCleanRecursiveWithAge(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "temp")
	write(t, filepath.Join(root, "old.tmp"), 100, 48*time.Hour)
	write(t, filepath.Join(root, "sub", "old2.log"), 50, 48*time.Hour)
	write(t, filepath.Join(root, "new.tmp"), 10, time.Minute)
	write(t, filepath.Join(root, "keep", "fresh.dat"), 10, time.Minute)
	old := time.Now().Add(-72 * time.Hour)
	os.Chtimes(filepath.Join(root, "sub"), old, old)

	e := NewEngine()
	tg := &Target{Rules: []Rule{{Root: root, Recursive: true, MinAge: 24 * time.Hour}}}

	s := e.Scan(context.Background(), tg, nil)
	if s.Files != 2 || s.Bytes != 150 {
		t.Fatalf("scan = %+v, want 2 files 150 bytes", s)
	}
	if !exists(filepath.Join(root, "old.tmp")) {
		t.Fatal("scan must not delete")
	}

	var logged []string
	c := e.Clean(context.Background(), tg, nil, func(p string) { logged = append(logged, p) })
	if c.Files != 2 || c.Bytes != 150 || len(logged) != 2 {
		t.Fatalf("clean = %+v logged=%v", c, logged)
	}
	if exists(filepath.Join(root, "old.tmp")) || exists(filepath.Join(root, "sub")) {
		t.Error("old files / empty folder should be gone")
	}
	for _, p := range []string{root, filepath.Join(root, "new.tmp"), filepath.Join(root, "keep", "fresh.dat")} {
		if !exists(p) {
			t.Errorf("%s should be kept", p)
		}
	}
}

func TestFilePatternsAndNonRecursive(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "explorer")
	write(t, filepath.Join(root, "thumbcache_256.db"), 10, 0)
	write(t, filepath.Join(root, "ThumbCache_96.DB"), 10, 0)
	write(t, filepath.Join(root, "other.db"), 10, 0)
	write(t, filepath.Join(root, "nested", "thumbcache_1.db"), 10, 0)

	e := NewEngine()
	tg := &Target{Rules: []Rule{{Root: root, Files: []string{"thumbcache_*.db"}}}}
	c := e.Clean(context.Background(), tg, nil, nil)
	if c.Files != 2 {
		t.Fatalf("clean = %+v, want 2", c)
	}
	if !exists(filepath.Join(root, "other.db")) || !exists(filepath.Join(root, "nested", "thumbcache_1.db")) {
		t.Error("non matching / nested files must be kept")
	}
}

func TestGlobRootsForProfiles(t *testing.T) {
	dir := t.TempDir()
	ud := filepath.Join(dir, "User Data")
	write(t, filepath.Join(ud, "Default", "Cache", "Cache_Data", "f_1"), 5, 0)
	write(t, filepath.Join(ud, "Profile 1", "Cache", "Cache_Data", "f_2"), 5, 0)
	write(t, filepath.Join(ud, "Default", "Bookmarks"), 5, 0)

	e := NewEngine()
	tg := &Target{Rules: []Rule{{Root: filepath.Join(ud, "*", "Cache", "Cache_Data"), Recursive: true}}}
	c := e.Clean(context.Background(), tg, nil, nil)
	if c.Files != 2 {
		t.Fatalf("clean = %+v, want 2", c)
	}
	if !exists(filepath.Join(ud, "Default", "Bookmarks")) {
		t.Error("bookmarks must be kept")
	}
}

func TestSymlinksAreNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	dir := t.TempDir()
	precious := filepath.Join(dir, "precious")
	write(t, filepath.Join(precious, "important.txt"), 10, 0)
	root := filepath.Join(dir, "temp")
	write(t, filepath.Join(root, "junk.tmp"), 10, 0)
	if err := os.Symlink(precious, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(precious, "important.txt"), filepath.Join(root, "filelink")); err != nil {
		t.Fatal(err)
	}

	e := NewEngine()
	tg := &Target{Rules: []Rule{{Root: root, Recursive: true}}}
	c := e.Clean(context.Background(), tg, nil, nil)
	if c.Files != 1 {
		t.Fatalf("clean = %+v, want 1", c)
	}
	if !exists(filepath.Join(precious, "important.txt")) {
		t.Fatal("followed a symlink and deleted the target")
	}
}

func TestDuplicateRulesCountOnce(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "c")
	write(t, filepath.Join(root, "a"), 7, 0)
	e := NewEngine()
	tg := &Target{Rules: []Rule{{Root: root}, {Root: root}}}
	if s := e.Scan(context.Background(), tg, nil); s.Files != 1 || s.Bytes != 7 {
		t.Fatalf("scan = %+v", s)
	}
}

func TestLockedFilesCountedAsFailed(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "c")
	write(t, filepath.Join(root, "a"), 7, 0)
	write(t, filepath.Join(root, "b"), 7, 0)
	e := NewEngine()
	e.remove = func(p string) error {
		if filepath.Base(p) == "a" {
			return os.ErrPermission
		}
		return os.Remove(p)
	}
	c := e.Clean(context.Background(), &Target{Rules: []Rule{{Root: root}}}, nil, nil)
	if c.Files != 1 || c.Failed != 1 || c.Bytes != 7 {
		t.Fatalf("clean = %+v", c)
	}
}

func TestCancel(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "c")
	for i := 0; i < 5; i++ {
		write(t, filepath.Join(root, string(rune('a'+i))), 1, 0)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := NewEngine().Clean(ctx, &Target{Rules: []Rule{{Root: root}}}, nil, nil)
	if res.Err == nil || res.Files != 0 {
		t.Fatalf("expected cancellation, got %+v", res)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 1536: "1.5 KB", 5 << 20: "5.0 MB", 3 << 30: "3.0 GB"}
	for in, want := range cases {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestNamedFilesAllowedOneLevelDeep(t *testing.T) {
	r := Rule{Root: "/x", Files: []string{"MEMORY.DMP"}}
	if !namedFilesOnly(r) {
		t.Fatal("expected named-files rule")
	}
	if namedFilesOnly(Rule{Root: "/x", Files: []string{"*.dmp"}}) || namedFilesOnly(Rule{Root: "/x"}) ||
		namedFilesOnly(Rule{Root: "/x", Files: []string{"a"}, Recursive: true}) {
		t.Fatal("wildcard, empty or recursive rules must not get the relaxed check")
	}
	one, root := "/x", "/"
	if runtime.GOOS == "windows" {
		one, root = `C:\x`, `C:\`
	}
	if !safeRootDepth(one, 1) || safeRootDepth(root, 1) || safeRootDepth(one, 2) {
		t.Fatal("depth check wrong")
	}
}

func TestAdminRulesSkippedWhenNotElevated(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "sys")
	write(t, filepath.Join(root, "a"), 3, 0)
	tg := &Target{Rules: []Rule{{Root: root, Admin: true}}}
	e := NewEngine()
	if s := e.Scan(context.Background(), tg, nil); s.Files != 0 {
		t.Fatalf("not elevated should skip, got %+v", s)
	}
	e.Elevated = true
	if s := e.Scan(context.Background(), tg, nil); s.Files != 1 {
		t.Fatalf("elevated should scan, got %+v", s)
	}
}

func TestSameFolderTwoPathsCountedOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real", "temp")
	write(t, filepath.Join(real, "a.tmp"), 9, 48*time.Hour)
	// Same folder reached through a linked parent, like an 8.3 short name.
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias", "temp")
	tg := &Target{Rules: []Rule{{Root: real, Recursive: true}, {Root: alias, Recursive: true}}}
	if s := NewEngine().Scan(context.Background(), &Target{Rules: []Rule{{Root: alias, Recursive: true}}}, nil); s.Files != 1 {
		t.Fatalf("alias alone should be scanned, got %+v", s)
	}
	if s := NewEngine().Scan(context.Background(), tg, nil); s.Files != 1 || s.Bytes != 9 {
		t.Fatalf("scan = %+v, want counted once", s)
	}
}

func TestRecentEmptyFoldersKept(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "temp")
	write(t, filepath.Join(root, "installer", "old.bin"), 1, 48*time.Hour)
	os.MkdirAll(filepath.Join(root, "justmade"), 0o755)
	old := time.Now().Add(-72 * time.Hour)
	os.Chtimes(filepath.Join(root, "installer"), old, old)
	NewEngine().Clean(context.Background(), &Target{Rules: []Rule{{Root: root, Recursive: true, MinAge: 24 * time.Hour}}}, nil, nil)
	if exists(filepath.Join(root, "installer")) {
		t.Error("old emptied folder should be removed")
	}
	if !exists(filepath.Join(root, "justmade")) {
		t.Error("new empty folder must be kept")
	}
}

func TestFilesPlural(t *testing.T) {
	if Files(1) != "1 file" || Files(0) != "0 files" || Files(3) != "3 files" {
		t.Fatal("plural wrong")
	}
}
