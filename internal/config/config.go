// Package config loads the small amount of environment configuration LabDash
// needs. Everything else is configured through the GUI and stored in SQLite.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Port    string
	DataDir string
	Secret  string
	Dev     bool
}

func Load() (*Config, error) {
	c := &Config{
		Port:    env("PORT", "8080"),
		DataDir: env("LABDASH_DATA_DIR", "/data"),
		Secret:  os.Getenv("LABDASH_SECRET"),
		Dev:     os.Getenv("LABDASH_DEV") == "1",
	}
	if err := os.MkdirAll(c.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	for _, sub := range []string{"uploads", "icons", "wallpapers", "favicons"} {
		if err := os.MkdirAll(filepath.Join(c.DataDir, sub), 0o750); err != nil {
			return nil, err
		}
	}
	if c.Secret == "" {
		s, err := loadOrCreateSecret(filepath.Join(c.DataDir, "secret.key"))
		if err != nil {
			return nil, err
		}
		slog.Warn("LABDASH_SECRET not set; using generated key stored in data dir (back it up with your data)")
		c.Secret = s
	}
	return c, nil
}

func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "labdash.db") }

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadOrCreateSecret(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	s := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		return "", fmt.Errorf("write secret: %w", err)
	}
	return s, nil
}
