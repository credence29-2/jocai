//go:build !windows

package regclean

import "errors"

var errNotWindows = errors.New("only supported on Windows")

func Scan([]string, bool) []Issue                   { return nil }
func Fix([]Issue, string) (int, int, string, error) { return 0, 0, "", errNotWindows }
func Backups(string) []Backup                       { return nil }
func Restore(string, string) error                  { return errNotWindows }

type Backup struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	When  string `json:"when"`
	Count int    `json:"count"`
}
