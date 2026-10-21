//go:build !windows

package systools

import "errors"

var errNotWindows = errors.New("only supported on Windows")

func Tasks() ([]Task, error)            { return nil, errNotWindows }
func SetTask(string, bool) error        { return errNotWindows }
func Services() ([]Service, error)      { return nil, errNotWindows }
func SetService(string, string) error   { return errNotWindows }
func ProblemDevices() ([]Device, error) { return nil, errNotWindows }
func KeyDrivers() ([]Driver, error)     { return nil, errNotWindows }
func WingetAvailable() bool             { return false }
func Updates() ([]Update, error)        { return nil, errNotWindows }
func UpdateApp(string) error            { return errNotWindows }
