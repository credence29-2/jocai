package startup

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestIsDisabled(t *testing.T) {
	cases := []struct {
		in   []byte
		want bool
	}{
		{nil, false},
		{[]byte{0x02, 0, 0, 0}, false},
		{[]byte{0x06, 0, 0, 0}, false},
		{[]byte{0x03, 0, 0, 0}, true},
		{[]byte{0x07}, true},
	}
	for _, c := range cases {
		if got := IsDisabled(c.in); got != c.want {
			t.Errorf("IsDisabled(%v) = %v", c.in, got)
		}
	}
}

func TestApprovedValueRoundTrip(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	on := ApprovedValue(true, now)
	off := ApprovedValue(false, now)
	if len(on) != 12 || len(off) != 12 || IsDisabled(on) || !IsDisabled(off) {
		t.Fatalf("bad values %x %x", on, off)
	}
	ft := binary.LittleEndian.Uint64(off[4:])
	back := time.Unix(0, int64(ft-116444736000000000)*100).UTC()
	if !back.Equal(now) {
		t.Errorf("filetime round trip = %v", back)
	}
}
