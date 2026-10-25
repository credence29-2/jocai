package main

import (
	"testing"
	"unsafe"
)

// Shell_NotifyIconW rejects a struct of the wrong size, so check it matches
// the Windows SDK's NOTIFYICONDATAW on 64-bit Windows.
func TestNotifyIconDataSize(t *testing.T) {
	if got := unsafe.Sizeof(notifyIconData{}); got != 976 {
		t.Fatalf("NOTIFYICONDATAW is %d bytes, want 976", got)
	}
}

func TestTruncate(t *testing.T) {
	if truncate("héllo\x00 world", 5) != "héll…" || truncate("hi", 5) != "hi" {
		t.Fatal(truncate("héllo\x00 world", 5))
	}
}
