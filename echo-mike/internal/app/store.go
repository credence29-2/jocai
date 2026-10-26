package app

import (
	"os"

	"github.com/credence29-2/jocai/echo-mike/internal/store"
)

// openStore opens the saved settings, or a throwaway folder in demo mode so
// sample accounts never mix with real ones.
func OpenStore(dir string, demoMode bool) (*store.Store, error) {
	if demoMode {
		tmp, err := os.MkdirTemp("", "echo-mike-demo-")
		if err != nil {
			return nil, err
		}
		dir = tmp
	}
	return store.Open(dir)
}
