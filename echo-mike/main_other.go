//go:build !windows

// Command echo-mike is Project Echo Mike. On Windows it opens its own window. Here it
// serves the same interface to your browser, for development.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/credence29-2/jocai/echo-mike/internal/app"
	"github.com/credence29-2/jocai/echo-mike/internal/store"
)

func main() {
	demoMode := flag.Bool("demo", false, "use sample accounts instead of real ones")
	dataDir := flag.String("data", store.Dir(), "folder for settings and sign-ins")
	noBrowser := flag.Bool("no-browser", false, "print the address instead of opening a browser")
	mcpMode := flag.Bool("mcp", false, "serve the MCP tools over stdin/stdout for Claude Desktop")
	flag.Parse()

	st, err := app.OpenStore(*dataDir, *demoMode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	a := app.New(st, *demoMode, openBrowser)
	if *mcpMode {
		if err := a.ServeMCP(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	u, err := a.Listen()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(u)
	if !*noBrowser {
		openBrowser(u)
	}
	select {}
}

func openBrowser(u string) error {
	cmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		cmd = "open"
	}
	return exec.Command(cmd, u).Start()
}
