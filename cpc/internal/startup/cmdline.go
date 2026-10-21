package startup

import "strings"

// ExePath pulls the program path out of a command line such as
//
//	"C:\Program Files\App\app.exe" --minimized
//	C:\Tools\thing.exe /background
func ExePath(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if end := strings.Index(cmd[1:], `"`); end >= 0 {
			return cmd[1 : end+1]
		}
		return strings.Trim(cmd, `"`)
	}
	lower := strings.ToLower(cmd)
	if i := strings.Index(lower, ".exe"); i >= 0 {
		return cmd[:i+4]
	}
	if i := strings.IndexByte(cmd, ' '); i >= 0 {
		return cmd[:i]
	}
	return cmd
}
