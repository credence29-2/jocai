//go:build !windows

package report

import "time"

func Gather(app, version, company, logsDir string, elevated bool) *Report {
	r := &Report{App: app, Version: version, Company: company, Generated: time.Now(), Elevated: elevated}
	r.Cleans = ReadCleans(logsDir, 5)
	r.Assess()
	return r
}
