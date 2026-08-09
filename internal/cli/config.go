// Package cli implements the AIHub CLI: server client, config storage, and
// installation of skills / expert packs / MCP profiles into Codex.
package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ConfigPath returns the path of the local CLI config file
// (override with $AIHUB_CONFIG, defaults to ~/.aihub/config.json).
func ConfigPath() string {
	if p := os.Getenv("AIHUB_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".aihub", "config.json")
	}
	return filepath.Join(home, ".aihub", "config.json")
}

// Config holds the CLI's persisted state: server endpoint and auth token.
type Config struct {
	ServerURL    string     `json:"server_url"`
	Username     string     `json:"username,omitempty"`
	Scopes       []string   `json:"scopes,omitempty"`
	Token        string     `json:"token,omitempty"`
	TokenID      int64      `json:"token_id,omitempty"`
	TokenExpires *time.Time `json:"token_expires,omitempty"`
}

// LoadConfig reads the config file. A missing file yields an empty config.
func LoadConfig() (*Config, error) {
	cfg := &Config{}
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save persists the config file with 0600 permissions.
func (c *Config) Save() error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// HasToken reports whether a token is stored.
func (c *Config) HasToken() bool { return c.Token != "" }

// Logout clears the auth state.
func (c *Config) Logout() {
	c.Token = ""
	c.TokenID = 0
	c.Username = ""
	c.Scopes = nil
	c.TokenExpires = nil
}
