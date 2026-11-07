// Package db opens the SQLite database and applies migrations.
package db

import (
	"database/sql"
	"fmt"
	"io"
	"log"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/bentonraymer/labdash/migrations"
)

func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_txlock=immediate", path)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// WAL allows concurrent readers alongside one writer; busy_timeout
	// serializes writers. _txlock=immediate makes every transaction take the
	// write lock up front, so a read-then-write transaction can't fail with
	// SQLITE_BUSY_SNAPSHOT when another writer commits in between. A small pool avoids self-deadlock when a handler
	// issues a query while another result set is still open.
	d.SetMaxOpenConns(4)
	if err := d.Ping(); err != nil {
		return nil, err
	}
	if err := Migrate(d); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func Migrate(d *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(log.New(io.Discard, "", 0))
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.Up(d, ".")
}
