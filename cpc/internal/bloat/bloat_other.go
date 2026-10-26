//go:build !windows

package bloat

import "errors"

func List() ([]App, error)      { return nil, errors.New("only supported on Windows") }
func Remove(string, bool) error { return errors.New("only supported on Windows") }
