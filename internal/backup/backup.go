// Package backup creates and restores zip archives of LabDash's data
// directory: a consistent SQLite snapshot plus uploads, custom icons and
// wallpapers. Restores are staged and applied at the next start.
package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Dirs copied into a backup (relative to the data dir).
var Dirs = []string{"uploads", "icons/custom", "wallpapers", "favicons"}

const (
	pendingDir = "restore-pending"
	dbName     = "labdash.db"
)

type Manifest struct {
	App       string `json:"app"`
	Version   string `json:"version"`
	CreatedAt string `json:"createdAt"`
}

// Write streams a backup zip to w.
func Write(ctx context.Context, db *sql.DB, dataDir, version string, w io.Writer) error {
	tmp := filepath.Join(dataDir, fmt.Sprintf(".backup-%d.db", time.Now().UnixNano()))
	defer os.Remove(tmp)
	// VACUUM INTO produces a consistent, compact copy without stopping writes.
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, tmp); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	zw := zip.NewWriter(w)
	m, _ := json.MarshalIndent(Manifest{App: "labdash", Version: version, CreatedAt: time.Now().UTC().Format(time.RFC3339)}, "", "  ")
	if err := addBytes(zw, "manifest.json", m); err != nil {
		return err
	}
	if err := addFile(zw, dbName, tmp); err != nil {
		return err
	}
	// The generated secret key (when LABDASH_SECRET isn't set) is needed to
	// decrypt stored credentials after a restore.
	if _, err := os.Stat(filepath.Join(dataDir, "secret.key")); err == nil {
		if err := addFile(zw, "secret.key", filepath.Join(dataDir, "secret.key")); err != nil {
			return err
		}
	}
	for _, d := range Dirs {
		root := filepath.Join(dataDir, d)
		_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(dataDir, p)
			return addFile(zw, filepath.ToSlash(rel), p)
		})
	}
	return zw.Close()
}

func addBytes(zw *zip.Writer, name string, b []byte) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	return err
}

func addFile(zw *zip.Writer, name, path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	st, _ := src.Stat()
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
	if st != nil {
		hdr.Modified = st.ModTime()
	}
	dst, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}

// Stage validates a backup zip and unpacks it into the pending-restore dir.
func Stage(dataDir string, zipPath string) (*Manifest, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, errors.New("not a valid zip file")
	}
	defer zr.Close()
	pending := filepath.Join(dataDir, pendingDir)
	_ = os.RemoveAll(pending)
	if err := os.MkdirAll(pending, 0o750); err != nil {
		return nil, err
	}
	var manifest *Manifest
	hasDB := false
	allowed := map[string]bool{"manifest.json": true, dbName: true, "secret.key": true}
	for _, f := range zr.File {
		name := filepath.ToSlash(filepath.Clean(f.Name))
		if f.FileInfo().IsDir() {
			continue
		}
		ok := allowed[name]
		for _, d := range Dirs {
			if strings.HasPrefix(name, d+"/") {
				ok = true
			}
		}
		// Reject anything unexpected, including path traversal.
		if !ok || strings.Contains(name, "..") || filepath.IsAbs(name) {
			os.RemoveAll(pending)
			return nil, fmt.Errorf("unexpected file in backup: %s", f.Name)
		}
		if err := extract(f, filepath.Join(pending, filepath.FromSlash(name))); err != nil {
			os.RemoveAll(pending)
			return nil, err
		}
		switch name {
		case dbName:
			hasDB = true
		case "manifest.json":
			b, _ := os.ReadFile(filepath.Join(pending, name))
			var m Manifest
			if json.Unmarshal(b, &m) == nil {
				manifest = &m
			}
		}
	}
	if !hasDB || manifest == nil || manifest.App != "labdash" {
		os.RemoveAll(pending)
		return nil, errors.New("this isn't a LabDash backup (missing manifest or database)")
	}
	if err := checkDB(filepath.Join(pending, dbName)); err != nil {
		os.RemoveAll(pending)
		return nil, err
	}
	return manifest, nil
}

func extract(f *zip.File, dst string) error {
	if f.UncompressedSize64 > 4<<30 {
		return errors.New("file in backup is too large")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, io.LimitReader(rc, 4<<30))
	return err
}

func checkDB(path string) error {
	d, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer d.Close()
	var res string
	if err := d.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		return fmt.Errorf("backup database failed its integrity check (%v %s)", err, res)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM goose_db_version`).Scan(&n); err != nil {
		return errors.New("backup database isn't a LabDash database")
	}
	return nil
}

// HasPending reports whether a restore is staged.
func HasPending(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, pendingDir, dbName))
	return err == nil
}

// ApplyPending swaps a staged restore into place. Call before opening the DB.
// The replaced database is kept in backups/ as a safety net.
func ApplyPending(dataDir string) (bool, error) {
	if !HasPending(dataDir) {
		return false, nil
	}
	pending := filepath.Join(dataDir, pendingDir)
	keep := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(keep, 0o750); err != nil {
		return false, err
	}
	cur := filepath.Join(dataDir, dbName)
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, filepath.Join(keep, "pre-restore-"+time.Now().Format("20060102-150405")+".db")); err != nil {
			return false, err
		}
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		os.Remove(cur + suffix)
	}
	if err := os.Rename(filepath.Join(pending, dbName), cur); err != nil {
		return false, err
	}
	if _, err := os.Stat(filepath.Join(pending, "secret.key")); err == nil {
		_ = os.Rename(filepath.Join(pending, "secret.key"), filepath.Join(dataDir, "secret.key"))
	}
	for _, d := range Dirs {
		src := filepath.Join(pending, d)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(dataDir, d)
		_ = os.RemoveAll(dst)
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return false, err
		}
		if err := os.Rename(src, dst); err != nil {
			return false, err
		}
	}
	return true, os.RemoveAll(pending)
}

// ── stored backups (scheduled or manual) ──────────────────────────────────

type Stored struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"createdAt"`
}

func Dir(dataDir string) string { return filepath.Join(dataDir, "backups") }

// SaveTo writes a backup zip into the backups dir and prunes old ones.
func SaveTo(ctx context.Context, db *sql.DB, dataDir, version string, keep int) (string, error) {
	dir := Dir(dataDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	name := "labdash-" + time.Now().Format("20060102-150405") + ".zip"
	tmp := filepath.Join(dir, "."+name)
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if err := Write(ctx, db, dataDir, version, f); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		return "", err
	}
	if keep > 0 {
		list, _ := List(dataDir)
		for i := keep; i < len(list); i++ {
			os.Remove(filepath.Join(dir, list[i].Name))
		}
	}
	return name, nil
}

// List returns stored backup zips, newest first.
func List(dataDir string) ([]Stored, error) {
	entries, err := os.ReadDir(Dir(dataDir))
	if errors.Is(err, fs.ErrNotExist) {
		return []Stored{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Stored{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "labdash-") || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Stored{Name: e.Name(), Size: info.Size(), CreatedAt: info.ModTime().UTC().Format(time.RFC3339)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// ValidName guards download/delete paths.
func ValidName(name string) bool {
	return strings.HasPrefix(name, "labdash-") && strings.HasSuffix(name, ".zip") && !strings.ContainsAny(name, `/\`) && !strings.Contains(name, "..")
}
