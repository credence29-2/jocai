package repair

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Progress receives output as a tool runs.
type Progress func(step int, total int, lines []string, pct int)

// Result is the outcome of a tool.
type Result struct {
	OK      bool   `json:"ok"`
	Text    string `json:"text"`
	Output  string `json:"output"`
	Reboot  bool   `json:"reboot"`
	Minutes int    `json:"minutes"`
}

// Run executes a tool's steps in order, streaming output.
func Run(ctx context.Context, t Tool, prog Progress) Result {
	start := time.Now()
	var all strings.Builder
	exitOK := true
	for i, st := range t.Steps {
		if ctx.Err() != nil {
			return Result{OK: false, Text: "Stopped.", Output: all.String()}
		}
		all.WriteString("> " + st.Name + " " + strings.Join(st.Args, " ") + "\r\n")
		out, err := runStep(ctx, st, func(partial string) {
			lines, pct := Lines(all.String() + partial)
			if prog != nil {
				prog(i, len(t.Steps), lines, pct)
			}
		})
		all.WriteString(out)
		if !strings.HasSuffix(out, "\n") {
			all.WriteString("\r\n")
		}
		if err != nil {
			all.WriteString("(" + err.Error() + ")\r\n")
			if !st.Optional {
				exitOK = false
				break
			}
		}
	}
	ok, text := Summary(t.ID, all.String(), exitOK)
	return Result{OK: ok, Text: text, Output: all.String(), Reboot: t.Reboot || strings.Contains(strings.ToLower(text), "restart"),
		Minutes: int(time.Since(start).Minutes())}
}

func runStep(ctx context.Context, st Step, partial func(string)) (string, error) {
	cmd := exec.CommandContext(ctx, st.Name, st.Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		return "", fmt.Errorf("couldn't start %s: %v", st.Name, err)
	}
	var raw []byte
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		last := time.Now()
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				raw = append(raw, buf[:n]...)
				if time.Since(last) > 250*time.Millisecond {
					last = time.Now()
					partial(Decode(raw))
				}
			}
			if err != nil {
				break
			}
		}
		close(done)
	}()
	err := cmd.Wait()
	pw.Close()
	<-done
	out := Decode(raw)
	partial(out)
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out, fmt.Errorf("exit code %d", ee.ExitCode())
		}
		return out, err
	}
	return out, nil
}
