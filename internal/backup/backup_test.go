package backup

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bentonraymer/labdash/internal/db"
)

func TestBackupRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, dbName))
	if err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO notes(title, body) VALUES ('Original', 'keep me')`)
	os.MkdirAll(filepath.Join(dir, "uploads"), 0o750)
	os.WriteFile(filepath.Join(dir, "uploads", "a.png"), []byte("img"), 0o640)

	name, err := SaveTo(ctx, d, dir, "test", 7)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := List(dir)
	if len(list) != 1 || list[0].Name != name {
		t.Fatalf("list = %+v", list)
	}

	// Change data after the backup, then restore.
	d.Exec(`UPDATE notes SET title = 'Changed'`)
	os.WriteFile(filepath.Join(dir, "uploads", "a.png"), []byte("changed"), 0o640)
	m, err := Stage(dir, filepath.Join(Dir(dir), name))
	if err != nil || m.App != "labdash" {
		t.Fatalf("stage: %v %v", m, err)
	}
	d.Close()
	applied, err := ApplyPending(dir)
	if err != nil || !applied {
		t.Fatalf("apply: %v %v", applied, err)
	}
	d2, err := db.Open(filepath.Join(dir, dbName))
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	var title string
	d2.QueryRow(`SELECT title FROM notes`).Scan(&title)
	if title != "Original" {
		t.Fatalf("restored title = %q", title)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uploads", "a.png")); string(b) != "img" {
		t.Fatalf("restored upload = %q", b)
	}
	if HasPending(dir) {
		t.Fatal("pending dir should be gone")
	}
	pre, _ := filepath.Glob(filepath.Join(dir, "backups", "pre-restore-*.db"))
	if len(pre) != 1 {
		t.Fatal("previous database should be kept as pre-restore")
	}
}

func TestStageRejectsBadArchives(t *testing.T) {
	dir := t.TempDir()
	mk := func(files map[string]string) string {
		p := filepath.Join(t.TempDir(), "x.zip")
		f, _ := os.Create(p)
		zw := zip.NewWriter(f)
		for n, c := range files {
			w, _ := zw.Create(n)
			w.Write([]byte(c))
		}
		zw.Close()
		f.Close()
		return p
	}
	cases := map[string]map[string]string{
		"traversal":   {"manifest.json": `{"app":"labdash"}`, "../evil": "x"},
		"no database": {"manifest.json": `{"app":"labdash"}`},
		"not sqlite":  {"manifest.json": `{"app":"labdash"}`, "labdash.db": "garbage"},
		"other app":   {"manifest.json": `{"app":"other"}`, "labdash.db": "x"},
	}
	for name, files := range cases {
		if _, err := Stage(dir, mk(files)); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
		if HasPending(dir) {
			t.Errorf("%s: rejected archive left a pending restore", name)
		}
	}
	if !ValidName("labdash-20261107-030000.zip") || ValidName("../labdash-x.zip") || ValidName("other.zip") {
		t.Error("ValidName wrong")
	}
}
