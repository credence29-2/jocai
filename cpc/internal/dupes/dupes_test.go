package dupes

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func w(t *testing.T, p, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindDuplicates(t *testing.T) {
	d := t.TempDir()
	big := string(make([]byte, 100*1024)) + "A"
	bigB := string(make([]byte, 100*1024)) + "B" // same size and same first 64 KB, different content
	w(t, filepath.Join(d, "Docs", "a.txt"), "hello world")
	w(t, filepath.Join(d, "Downloads", "a (1).txt"), "hello world")
	w(t, filepath.Join(d, "Docs", "other.txt"), "hello there") // same size, different content
	w(t, filepath.Join(d, "Pics", "x.bin"), big)
	w(t, filepath.Join(d, "Pics", "x copy.bin"), big)
	w(t, filepath.Join(d, "Pics", "y.bin"), bigB)
	w(t, filepath.Join(d, "AppData", "dup.txt"), "hello world") // skipped folder
	w(t, filepath.Join(d, "tiny1"), "z")
	w(t, filepath.Join(d, "tiny2"), "z")

	groups, err := Find(context.Background(), []string{d, filepath.Join(d, "Docs")}, Options{MinSize: 2, SkipDirs: DefaultSkip}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d groups: %+v", len(groups), groups)
	}
	if groups[0].Size != int64(len(big)) || len(groups[0].Files) != 2 {
		t.Fatalf("biggest group wrong: %+v", groups[0])
	}
	if len(groups[1].Files) != 2 {
		t.Fatalf("text group wrong: %+v", groups[1])
	}
	if Wasted(groups) != int64(len(big))+11 {
		t.Fatalf("wasted = %d", Wasted(groups))
	}
	// IDs are unique across groups.
	ids := map[int]bool{}
	for _, g := range groups {
		for _, f := range g.Files {
			if ids[f.ID] {
				t.Fatal("duplicate file id")
			}
			ids[f.ID] = true
		}
	}
}

func TestSelectionKeepsOneCopy(t *testing.T) {
	groups := []Group{{Size: 5, Files: []File{{ID: 0, Path: "/a"}, {ID: 1, Path: "/b"}}}, {Size: 3, Files: []File{{ID: 2, Path: "/c"}, {ID: 3, Path: "/d"}}}}
	paths, err := CheckSelection(groups, []int{1, 2})
	if err != nil || len(paths) != 2 {
		t.Fatalf("paths=%v err=%v", paths, err)
	}
	if _, err := CheckSelection(groups, []int{0, 1}); err == nil {
		t.Fatal("deleting every copy must be refused")
	}
}

func TestCancel(t *testing.T) {
	d := t.TempDir()
	w(t, filepath.Join(d, "a"), "same")
	w(t, filepath.Join(d, "b"), "same")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Find(ctx, []string{d}, Options{}, nil); err == nil {
		t.Fatal("expected cancellation")
	}
}
