package health

import "testing"

func TestDiskRate(t *testing.T) {
	cases := []struct {
		d     Disk
		level string
	}{
		{Disk{Status: "Healthy"}, "good"},
		{Disk{Status: "Healthy", Wear: 75}, "warn"},
		{Disk{Status: "Healthy", Wear: 95}, "bad"},
		{Disk{Status: "Healthy", ReadErrors: 3}, "bad"},
		{Disk{Status: "Warning"}, "warn"},
		{Disk{Status: "Healthy", Temperature: 70}, "warn"},
		{Disk{Status: "Healthy", PowerOnHours: 50000}, "warn"},
		{Disk{Status: "Unknown", Wear: -1}, "good"},
	}
	for _, c := range cases {
		c.d.Rate()
		if c.d.Level != c.level {
			t.Errorf("%+v -> %s, want %s (%s)", c.d, c.d.Level, c.level, c.d.Text)
		}
	}
}

func TestBatteryRate(t *testing.T) {
	b := Battery{Present: true, DesignCapacity: 50000, FullCapacity: 30000}
	b.Rate()
	if b.HealthPercent != 60 || b.Level != "warn" {
		t.Fatalf("got %+v", b)
	}
	b = Battery{Present: true, DesignCapacity: 50000, FullCapacity: 20000}
	b.Rate()
	if b.Level != "bad" {
		t.Fatalf("got %+v", b)
	}
	b = Battery{Present: false}
	b.Rate()
	if b.Level != "" {
		t.Fatal("desktop should have no level")
	}
	b = Battery{Present: true, DesignCapacity: 40000, FullCapacity: 44000}
	b.Rate()
	if b.HealthPercent != 100 || b.Level != "good" {
		t.Fatalf("got %+v", b)
	}
}
