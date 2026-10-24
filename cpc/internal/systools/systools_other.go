//go:build !windows

package systools

import "errors"

var errNotWindows = errors.New("only supported on Windows")

func Tasks() ([]Task, error)              { return nil, errNotWindows }
func SetTask(string, bool) error          { return errNotWindows }
func Services() ([]Service, error)        { return nil, errNotWindows }
func SetService(string, string) error     { return errNotWindows }
func ProblemDevices() ([]Device, error)   { return nil, errNotWindows }
func InstalledDrivers() ([]Driver, error) { return nil, errNotWindows }
func SystemMaker() string                 { return "" }
func DriverUpdates() ([]DriverUpdate, []DriverUpdate, error) {
	return nil, nil, errNotWindows
}
func InstallDriverUpdate(string) (DriverInstallResult, error) {
	return DriverInstallResult{}, errNotWindows
}
func SetDriverUpdateHidden(string, bool) error { return errNotWindows }
func RestorePoint(string) (string, error)      { return "", errNotWindows }

// DriverInstallResult is what installing one driver update did.
type DriverInstallResult struct {
	OK      bool   `json:"ok"`
	Text    string `json:"text"`
	Reboot  bool   `json:"reboot"`
	HResult int64  `json:"hresult"`
}

func WingetAvailable() bool        { return false }
func Updates() ([]Update, error)   { return nil, errNotWindows }
func UpdateApp(string) error       { return errNotWindows }
func OpenSelfUpdater(string) error { return errNotWindows }
