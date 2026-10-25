// Package store keeps accounts, identities and settings in a JSON file and
// sign-in tokens in a separate encrypted file.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

// Account is one connected mailbox.
type Account struct {
	ID       string `json:"id"`
	Provider string `json:"provider"` // gmail, outlook or demo
	Email    string `json:"email"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	// NeedsReconnect is set when the provider rejected the saved sign-in.
	NeedsReconnect bool `json:"needsReconnect,omitempty"`
}

// Settings are app-wide preferences.
type Settings struct {
	GoogleClientID     string `json:"googleClientId"`
	GoogleClientSecret string `json:"googleClientSecret"`
	MicrosoftClientID  string `json:"microsoftClientId"`
	// DefaultIdentity is the From address for new messages.
	DefaultIdentity string `json:"defaultIdentity"`
	// LoadImages shows remote images without asking. Off by default so
	// tracking pixels don't report that a message was opened.
	LoadImages bool `json:"loadImages"`
	// LightMessages shows email bodies and the compose window on white.
	// Off by default, so they match the dark app.
	LightMessages bool `json:"lightMessages"`
}

// Data is everything in config.json.
type Data struct {
	// Version is the data layout version; see Migrate.
	Version    int             `json:"version"`
	Settings   Settings        `json:"settings"`
	Accounts   []Account       `json:"accounts"`
	Identities []mail.Identity `json:"identities"`
}

// Store is safe for concurrent use.
type Store struct {
	dir string

	mu      sync.Mutex
	data    Data
	secrets map[string]string
}

// Dir is where Project Echo Mike keeps its files: %APPDATA%\ProjectEchoMike on
// Windows.
func Dir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "ProjectEchoMike")
}

// Open loads the store in dir, creating it if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, secrets: map[string]string{}}
	if b, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if b, err := os.ReadFile(filepath.Join(dir, "tokens.bin")); err == nil {
		// Sign-ins copied from another PC or Windows account can't be
		// decrypted. Start without them; each account then asks to be
		// reconnected.
		if plain, err := unprotect(b); err == nil {
			json.Unmarshal(plain, &s.secrets)
		}
	}
	return s, nil
}

// Get returns a copy of the data.
func (s *Store) Get() Data {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.data)
	var d Data
	json.Unmarshal(b, &d)
	return d
}

// Update changes the data under the lock and saves it.
func (s *Store) Update(fn func(d *Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.data); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(s.dir, "config.json"), b)
}

// currentVersion is the data layout Migrate brings older files up to.
const currentVersion = 1

// Migrate updates data saved by older versions.
func (s *Store) Migrate() error {
	if s.Get().Version >= currentVersion {
		return nil
	}
	return s.Update(func(d *Data) error {
		if d.Version < 1 {
			// 1.0.0 imported Gmail addresses with their signature left off
			// replies and forwards. Turn it on, as new imports now do.
			for i := range d.Identities {
				if d.Identities[i].Imported {
					d.Identities[i].SignatureOnReplies = true
				}
			}
		}
		d.Version = currentVersion
		return nil
	})
}

// Secret returns an account's saved refresh token.
func (s *Store) Secret(accountID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secrets[accountID]
}

// SetSecret saves or, with "", removes an account's refresh token.
func (s *Store) SetSecret(accountID, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v == "" {
		delete(s.secrets, accountID)
	} else {
		s.secrets[accountID] = v
	}
	b, _ := json.Marshal(s.secrets)
	enc, err := protect(b)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(s.dir, "tokens.bin"), enc)
}

// writeFile replaces path atomically so a crash can't leave half a file.
func writeFile(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// NewID returns a short random ID.
func NewID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}
