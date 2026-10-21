//go:build !windows

package programs

import "errors"

func List() []Program              { return nil }
func RunUninstaller(Program) error { return errors.New("only supported on Windows") }
