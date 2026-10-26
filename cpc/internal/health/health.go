// Package health reads drive and battery condition.
package health

import "fmt"

// Disk is one physical drive.
type Disk struct {
	Name         string `json:"name"`
	Model        string `json:"model"`
	MediaType    string `json:"mediaType"` // SSD, HDD, NVMe, Unspecified
	Bus          string `json:"bus"`
	Size         int64  `json:"size"`
	Status       string `json:"status"` // Healthy, Warning, Unhealthy, Unknown
	Wear         int    `json:"wear"`   // percent used, -1 unknown
	Temperature  int    `json:"temperature"`
	PowerOnHours int64  `json:"powerOnHours"`
	ReadErrors   int64  `json:"readErrors"`
	WriteErrors  int64  `json:"writeErrors"`
	// Filled in by Rate.
	Level string `json:"level"` // good, warn, bad
	Text  string `json:"text"`
}

// Battery is the laptop battery, if any.
type Battery struct {
	Present        bool   `json:"present"`
	Charge         int    `json:"charge"` // percent now
	DesignCapacity int64  `json:"designCapacity"`
	FullCapacity   int64  `json:"fullCapacity"`
	HealthPercent  int    `json:"healthPercent"` // full / design
	CycleCount     int    `json:"cycleCount"`
	Status         string `json:"status"`
	Level          string `json:"level"`
	Text           string `json:"text"`
}

// Rate fills Level and Text from the raw numbers.
func (d *Disk) Rate() {
	switch {
	case d.Status == "Unhealthy" || d.ReadErrors > 0 || d.WriteErrors > 0:
		d.Level, d.Text = "bad", "This drive is reporting errors. Back it up now and plan to replace it."
	case d.Status == "Warning":
		d.Level, d.Text = "warn", "Windows reports a warning for this drive. Back up important files."
	case d.Wear >= 90:
		d.Level, d.Text = "bad", fmt.Sprintf("%d%% of this SSD's rated life is used. Replace it soon.", d.Wear)
	case d.Wear >= 70:
		d.Level, d.Text = "warn", fmt.Sprintf("%d%% of this SSD's rated life is used.", d.Wear)
	case d.Temperature >= 65:
		d.Level, d.Text = "warn", fmt.Sprintf("Running hot at %d°C. Check airflow.", d.Temperature)
	case d.Status == "Healthy":
		d.Level, d.Text = "good", "Healthy."
		if d.PowerOnHours > 0 {
			years := float64(d.PowerOnHours) / 8760
			if years >= 5 {
				d.Level, d.Text = "warn", fmt.Sprintf("Healthy, but it has %.0f years of running time. Keep backups current.", years)
			}
		}
	default:
		d.Level, d.Text = "good", "No problems reported."
	}
}

// Rate fills Level and Text for a battery.
func (b *Battery) Rate() {
	if !b.Present {
		b.Level, b.Text = "", "No battery, this is a desktop or a server."
		return
	}
	if b.DesignCapacity > 0 && b.FullCapacity > 0 {
		b.HealthPercent = int(float64(b.FullCapacity) * 100 / float64(b.DesignCapacity))
		if b.HealthPercent > 100 {
			b.HealthPercent = 100
		}
	}
	switch {
	case b.HealthPercent > 0 && b.HealthPercent < 50:
		b.Level, b.Text = "bad", fmt.Sprintf("Holds %d%% of its original charge. It's worn out and worth replacing.", b.HealthPercent)
	case b.HealthPercent > 0 && b.HealthPercent < 75:
		b.Level, b.Text = "warn", fmt.Sprintf("Holds %d%% of its original charge. Expect shorter run time.", b.HealthPercent)
	case b.HealthPercent > 0:
		b.Level, b.Text = "good", fmt.Sprintf("Holds %d%% of its original charge.", b.HealthPercent)
	default:
		b.Level, b.Text = "good", "Battery present. Windows didn't report its wear."
	}
}
