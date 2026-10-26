//go:build !windows

package repair

import "context"

type Progress func(step int, total int, lines []string, pct int)

type Result struct {
	OK      bool   `json:"ok"`
	Text    string `json:"text"`
	Output  string `json:"output"`
	Reboot  bool   `json:"reboot"`
	Minutes int    `json:"minutes"`
}

func Run(context.Context, Tool, Progress) Result {
	return Result{Text: "only supported on Windows"}
}
