//go:build !windows

package install

func Current(runningExe string) Info {
	return Info{Dir: Dir(), Exe: Exe(), ThisCopy: IsInstalledCopy(runningExe), Portable: runningExe}
}
func Register(string) error                       { return ErrNotWindows }
func Install(_, _ string, _ bool, _ string) error { return ErrNotWindows }
func AddDesktopShortcut() error                   { return ErrNotWindows }
func Uninstall(_, _ string, _ bool) error         { return ErrNotWindows }
func RemoveLater(string)                          {}
