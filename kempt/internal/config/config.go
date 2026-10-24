// Package config loads kempt.json from the data folder, creating it with fresh
// secrets on first run.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	// AppName is what people see in the app, browser tab and notifications.
	AppName string `json:"app_name"`
	// Domain is the public name people type to reach the server, like
	// mychat.duckdns.org. Empty means "no domain": HTTP only, for testing or
	// when something else (Tailscale, a reverse proxy) handles HTTPS.
	Domain string `json:"domain"`
	// HTTPS is "auto" (get a Let's Encrypt certificate for Domain on ports 80
	// and 443), "off" (plain HTTP on HTTPPort), or "proxy" (plain HTTP on
	// HTTPPort, trusting X-Forwarded-* from a reverse proxy in front).
	HTTPS    string `json:"https"`
	HTTPPort int    `json:"http_port"`
	// ACMEEmail is optional; Let's Encrypt uses it for expiry warnings.
	ACMEEmail string `json:"acme_email"`

	// Media ports. People outside your home need these forwarded on the router.
	RTCTCPPort int `json:"rtc_tcp_port"`
	RTCUDPPort int `json:"rtc_udp_port"`
	// PublicIP overrides the address advertised for media. Empty means look it
	// up with STUN at startup, which is right for almost every home network.
	PublicIP string `json:"public_ip"`
	// LANOnly skips the public IP lookup entirely.
	LANOnly bool `json:"lan_only"`

	// DuckDNSToken keeps a duckdns.org Domain pointed at this house when the
	// ISP changes the public IP.
	DuckDNSToken string `json:"duckdns_token"`

	// OpenRegistration lets anyone with the URL make an account. Off by
	// default: after the first account, people need an invite link.
	OpenRegistration bool `json:"open_registration"`
	// MaxUploadMB caps a single attachment.
	MaxUploadMB int `json:"max_upload_mb"`

	LiveKitKey    string `json:"livekit_key"`
	LiveKitSecret string `json:"livekit_secret"`

	path string
}

func Load(dataDir string) (*Config, error) {
	path := filepath.Join(dataDir, "kempt.json")
	c := &Config{AppName: "Project Victor Charlie", HTTPS: "off", HTTPPort: 8080, RTCTCPPort: 7881, RTCUDPPort: 7882, MaxUploadMB: 100}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	c.path = path
	dirty := err != nil
	if c.LiveKitKey == "" {
		c.LiveKitKey = "kempt" + randHex(6)
		dirty = true
	}
	if c.LiveKitSecret == "" {
		c.LiveKitSecret = randHex(32)
		dirty = true
	}
	switch c.HTTPS {
	case "auto":
		if c.Domain == "" {
			return nil, errors.New(`kempt.json: "https": "auto" needs a "domain"`)
		}
	case "off", "proxy":
	default:
		return nil, fmt.Errorf(`kempt.json: "https" must be "auto", "off" or "proxy", not %q`, c.HTTPS)
	}
	if dirty {
		if err := c.Save(); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *Config) Save() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, append(b, '\n'), 0o600)
}

func (c *Config) Path() string { return c.path }

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
