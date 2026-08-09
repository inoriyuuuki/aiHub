// Package cli implements the aihub local client: REST client, Codex adapter
// and stdio MCP server.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config is the CLI's local credentials/config file (~/.aihub/config.json).
type Config struct {
	ServerURL    string     `json:"serverUrl"`
	Username     string     `json:"username"`
	Token        string     `json:"token"`
	TokenID      int64      `json:"tokenId,omitempty"`
	Scopes       []string   `json:"scopes"`
	TokenCreated time.Time  `json:"tokenCreatedAt"`
	TokenExpires *time.Time `json:"tokenExpiresAt,omitempty"`
}

// ConfigPath returns the CLI config path honoring AIHUB_CONFIG_DIR.
func ConfigPath() string {
	if dir := os.Getenv("AIHUB_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "aihub-config.json"
	}
	return filepath.Join(home, ".aihub", "config.json")
}

// LoadConfig reads the CLI config (missing file => empty config).
func LoadConfig() (*Config, error) {
	p := ConfigPath()
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config %s 无效: %w", p, err)
	}
	return &c, nil
}

// Save writes the config atomically with 0600 permissions.
func (c *Config) Save() error {
	p := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite0600(p, data)
}

// HasToken reports whether a valid token is stored.
func (c *Config) HasToken() bool {
	if c.Token == "" {
		return false
	}
	if c.TokenExpires != nil && time.Now().After(*c.TokenExpires) {
		return false
	}
	return true
}

// Logout clears credentials.
func (c *Config) Logout() {
	c.Token = ""
	c.Scopes = nil
	c.TokenExpires = nil
	c.Username = ""
}
