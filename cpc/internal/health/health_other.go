//go:build !windows

package health

import "errors"

func Disks() ([]Disk, error) { return nil, errors.New("only supported on Windows") }
func BatteryInfo() Battery   { b := Battery{}; b.Rate(); return b }
