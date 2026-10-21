//go:build !windows

package startup

import "errors"

type source struct{}

func List() []Entry                { return nil }
func SetEnabled(Entry, bool) error { return errors.New("only supported on Windows") }
func Target(e Entry) string        { return ExePath(e.Command) }
