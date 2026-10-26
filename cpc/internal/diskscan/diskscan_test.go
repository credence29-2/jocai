package diskscan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func w(t *testing.T, p string, n int) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan(t *testing.T) {
	d := t.TempDir()
	w(t, filepath.Join(d, "Videos", "big.mp4"), 5000)
	w(t, filepath.Join(d, "Videos", "small.mp4"), 100)
	w(t, filepath.Join(d, "Docs", "a.pdf"), 300)
	w(t, filepath.Join(d, "Docs", "sub", "b.docx"), 200)
	w(t, filepath.Join(d, "root.txt"), 50)
	os.MkdirAll(filepath.Join(d, "Empty"), 0o755)

	tr, err := Scan(context.Background(), d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Files != 5 || tr.Bytes != 5650 || tr.Root.Size != 5650 {
		t.Fatalf("totals files=%d bytes=%d", tr.Files, tr.Bytes)
	}
	if tr.Root.Children[0].Name != "Videos" || tr.Root.Children[0].Size != 5100 {
		t.Fatalf("children not sorted by size: %+v", tr.Root.Children[0])
	}
	if tr.Largest[0].Path != filepath.Join(d, "Videos", "big.mp4") || tr.Largest[0].Type != "Videos" {
		t.Fatalf("largest = %+v", tr.Largest[0])
	}
	if tr.Types[0].Type != "Videos" || tr.Types[0].Size != 5100 {
		t.Fatalf("types = %+v", tr.Types)
	}
	docs := tr.Node(filepath.Join(d, "docs")) // case-insensitive lookup
	if docs == nil || docs.Size != 500 || docs.Files != 2 || len(docs.Top) != 1 {
		t.Fatalf("docs node = %+v", docs)
	}
	if !tr.Contains(filepath.Join(d, "Docs", "a.pdf")) || tr.Contains(filepath.Join(d+"2", "x")) {
		t.Fatal("Contains wrong")
	}

	tr.Forget(filepath.Join(d, "Videos", "big.mp4"), 5000)
	if tr.Bytes != 650 || tr.Root.Size != 650 || tr.Node(filepath.Join(d, "Videos")).Size != 100 || len(tr.Largest) != 4 {
		t.Fatalf("after forget: bytes=%d root=%d largest=%d", tr.Bytes, tr.Root.Size, len(tr.Largest))
	}
}

func TestTypeOf(t *testing.T) {
	if TypeOf("x.MP4") != "Videos" || TypeOf("x.exe") != "Installers" || TypeOf("x.weird") != "Other" || TypeOf("noext") != "Other" {
		t.Fatal("TypeOf wrong")
	}
}

func TestCancel(t *testing.T) {
	d := t.TempDir()
	w(t, filepath.Join(d, "a"), 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, d, nil); err == nil {
		t.Fatal("expected cancellation")
	}
}
