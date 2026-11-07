// Package integrations stores third-party credentials (Pushover, NPM,
// Proxmox) with secrets encrypted at rest.
package integrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/bentonraymer/labdash/internal/secrets"
)

var ErrNotConfigured = errors.New("integration not configured")

type Store struct {
	DB  *sql.DB
	Box *secrets.Box
}

// Get loads an integration. config receives the plain JSON config and secret
// the decrypted secret JSON; either may be nil.
func (s *Store) Get(ctx context.Context, kind string, config, secret any) (enabled bool, err error) {
	var cfg string
	var sealed []byte
	err = s.DB.QueryRowContext(ctx, `SELECT enabled, config, secret FROM integrations WHERE kind = ?`, kind).Scan(&enabled, &cfg, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotConfigured
	}
	if err != nil {
		return false, err
	}
	if config != nil {
		if err := json.Unmarshal([]byte(cfg), config); err != nil {
			return false, err
		}
	}
	if secret != nil && len(sealed) > 0 {
		plain, err := s.Box.Open(sealed)
		if err != nil {
			return false, errors.New("can't decrypt stored credentials — was LABDASH_SECRET changed?")
		}
		if err := json.Unmarshal(plain, secret); err != nil {
			return false, err
		}
	}
	return enabled, nil
}

// Put saves an integration. A nil secret keeps the existing stored secret.
func (s *Store) Put(ctx context.Context, kind string, enabled bool, config, secret any) error {
	cfg, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if secret == nil {
		_, err = s.DB.ExecContext(ctx, `INSERT INTO integrations(kind, enabled, config, updated_at) VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
			ON CONFLICT(kind) DO UPDATE SET enabled = excluded.enabled, config = excluded.config, updated_at = excluded.updated_at`,
			kind, enabled, string(cfg))
		return err
	}
	plain, err := json.Marshal(secret)
	if err != nil {
		return err
	}
	sealed, err := s.Box.Seal(plain)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO integrations(kind, enabled, config, secret, updated_at) VALUES (?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(kind) DO UPDATE SET enabled = excluded.enabled, config = excluded.config, secret = excluded.secret, updated_at = excluded.updated_at`,
		kind, enabled, string(cfg), sealed)
	return err
}
