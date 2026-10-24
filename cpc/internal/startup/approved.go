// Package startup lists and toggles programs that start with Windows.
//
// Kempt uses the same "StartupApproved" registry flags as Task Manager, so an
// item disabled here shows as disabled in Task Manager and the other way
// round. Nothing is ever deleted, so every change can be undone.
package startup

import (
	"encoding/binary"
	"time"
)

// Entry is one startup item.
type Entry struct {
	Name       string // value name or shortcut file name
	Command    string // command line or shortcut path
	Location   string // human readable source
	Enabled    bool
	NeedsAdmin bool

	src source
}

// IsDisabled decodes a StartupApproved value. Task Manager writes 0x02 or
// 0x06 in the first byte for enabled and an odd value (0x03, 0x07) for
// disabled. Missing or empty data means enabled.
func IsDisabled(data []byte) bool {
	return len(data) > 0 && data[0]&1 == 1
}

// ApprovedValue builds the 12 byte value Task Manager writes.
func ApprovedValue(enabled bool, now time.Time) []byte {
	b := make([]byte, 12)
	if enabled {
		b[0] = 0x02
		return b
	}
	b[0] = 0x03
	binary.LittleEndian.PutUint64(b[4:], toFiletime(now))
	return b
}

func toFiletime(t time.Time) uint64 {
	// 100ns intervals since 1601-01-01 UTC.
	const epochDiff = 116444736000000000
	return uint64(t.UnixNano()/100) + epochDiff
}
