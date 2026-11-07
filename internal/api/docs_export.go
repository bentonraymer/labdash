package api

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type exportNote struct {
	id        int64
	title     string
	body      string
	folderID  *int64
	service   string
	tags      []string
	createdAt string
	updatedAt string
	pinned    bool
}

type exportFolder struct {
	id     int64
	parent *int64
	name   string
}

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

// safeName makes a title usable as a file/folder name on every OS.
func safeName(s, fallback string) string {
	s = strings.TrimSpace(unsafeName.ReplaceAllString(s, "-"))
	s = strings.Trim(s, ". -")
	if len(s) > 80 {
		s = strings.TrimSpace(s[:80])
	}
	if s == "" {
		return fallback
	}
	return s
}

var uploadLink = regexp.MustCompile(`/media/uploads/([a-f0-9]{24}(?:\.[a-z0-9]{1,9})?)`)

// planDocsExport decides where every note goes in the archive:
//
//	<Folder>/<Subfolder>/<Title>.md   notes in folders
//	Services/<Service>.md             service docs not in a folder
//	Notes/<Title>.md                  everything else
//
// Paths are unique (duplicates get " (2)", " (3)"…).
func planDocsExport(notes []exportNote, folders map[int64]exportFolder) map[int64]string {
	folderPath := func(id int64) string {
		var parts []string
		seen := map[int64]bool{}
		for cur, ok := folders[id]; ok && !seen[cur.id] && len(parts) < 10; cur, ok = folders[*cur.parent] {
			seen[cur.id] = true
			parts = append([]string{safeName(cur.name, "Folder")}, parts...)
			if cur.parent == nil {
				break
			}
		}
		return path.Join(parts...)
	}
	sorted := append([]exportNote(nil), notes...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].id < sorted[j].id })
	used := map[string]bool{}
	out := map[int64]string{}
	for _, n := range sorted {
		var dir, base string
		switch {
		case n.folderID != nil && folders[*n.folderID].name != "":
			dir, base = folderPath(*n.folderID), safeName(n.title, "Untitled")
		case n.service != "":
			dir, base = "Services", safeName(n.service, "Service")
			if !strings.EqualFold(n.title, n.service) {
				base = safeName(n.service+" - "+n.title, "Service")
			}
		default:
			dir, base = "Notes", safeName(n.title, "Untitled")
		}
		p := path.Join(dir, base+".md")
		for i := 2; used[strings.ToLower(p)]; i++ {
			p = path.Join(dir, fmt.Sprintf("%s (%d).md", base, i))
		}
		used[strings.ToLower(p)] = true
		out[n.id] = p
	}
	return out
}

// rewriteUploads points /media/uploads/x links at attachments/x relative to
// the note's location, and returns the attachment files referenced.
func rewriteUploads(body, notePath string) (string, []string) {
	depth := strings.Count(notePath, "/")
	prefix := strings.Repeat("../", depth) + "attachments/"
	var files []string
	out := uploadLink.ReplaceAllStringFunc(body, func(m string) string {
		name := uploadLink.FindStringSubmatch(m)[1]
		files = append(files, name)
		return prefix + name
	})
	return out, files
}

func yamlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(s) + `"`
}

func frontMatter(n exportNote) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\n", yamlString(n.title))
	if n.service != "" {
		fmt.Fprintf(&b, "service: %s\n", yamlString(n.service))
	}
	if len(n.tags) > 0 {
		q := make([]string, len(n.tags))
		for i, t := range n.tags {
			q[i] = yamlString(t)
		}
		fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(q, ", "))
	}
	if n.pinned {
		b.WriteString("pinned: true\n")
	}
	fmt.Fprintf(&b, "created: %s\nupdated: %s\n---\n\n", n.createdAt, n.updatedAt)
	return b.String()
}

func (s *Server) loadExport(ctx context.Context) ([]exportNote, map[int64]exportFolder, map[string]string, error) {
	folders := map[int64]exportFolder{}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, parent_id, name FROM folders`)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var f exportFolder
		var p sql.NullInt64
		if rows.Scan(&f.id, &p, &f.name) == nil {
			if p.Valid {
				f.parent = &p.Int64
			}
			folders[f.id] = f
		}
	}
	rows.Close()

	tags, err := s.noteTags(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT n.id, n.title, n.body, n.folder_id, COALESCE(sv.name, ''), n.created_at, n.updated_at, n.pinned
		FROM notes n LEFT JOIN services sv ON sv.id = n.service_id`)
	if err != nil {
		return nil, nil, nil, err
	}
	var notes []exportNote
	for rows.Next() {
		var n exportNote
		var f sql.NullInt64
		if err := rows.Scan(&n.id, &n.title, &n.body, &f, &n.service, &n.createdAt, &n.updatedAt, &n.pinned); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		if f.Valid {
			n.folderID = &f.Int64
		}
		n.tags = tags[n.id]
		notes = append(notes, n)
	}
	rows.Close()

	names := map[string]string{} // stored upload name → original filename
	rows, err = s.DB.QueryContext(ctx, `SELECT path, filename FROM attachments`)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var p, fn string
		if rows.Scan(&p, &fn) == nil {
			names[p] = fn
		}
	}
	rows.Close()
	return notes, folders, names, nil
}

// handleExportDocs streams every note as a sorted zip of markdown files,
// with an index and the attachments the notes reference.
func (s *Server) handleExportDocs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	notes, folders, _, err := s.loadExport(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	root := "homelab-docs-" + time.Now().Format("2006-01-02")
	paths := planDocsExport(notes, folders)

	type file struct {
		name string
		body []byte
	}
	var files []file
	attachments := map[string]bool{}
	for _, n := range notes {
		p := paths[n.id]
		body, refs := rewriteUploads(n.body, p)
		for _, a := range refs {
			attachments[a] = true
		}
		files = append(files, file{p, []byte(frontMatter(n) + body)})
	}
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].name) < strings.ToLower(files[j].name) })

	// README index, grouped by directory.
	var idx strings.Builder
	fmt.Fprintf(&idx, "# Homelab docs\n\nExported from LabDash on %s · %d notes\n", time.Now().Format("2006-01-02 15:04"), len(notes))
	lastDir := "\x00"
	for _, f := range files {
		dir := path.Dir(f.name)
		if dir != lastDir {
			fmt.Fprintf(&idx, "\n## %s\n\n", dir)
			lastDir = dir
		}
		title := strings.TrimSuffix(path.Base(f.name), ".md")
		fmt.Fprintf(&idx, "- [%s](%s)\n", title, strings.ReplaceAll(f.name, " ", "%20"))
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+root+`.zip"`)
	zw := zip.NewWriter(w)
	put := func(name string, data []byte) error {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: root + "/" + name, Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			return err
		}
		_, err = fw.Write(data)
		return err
	}
	if err := put("README.md", []byte(idx.String())); err != nil {
		return
	}
	for _, f := range files {
		if err := put(f.name, f.body); err != nil {
			return
		}
	}
	names := make([]string, 0, len(attachments))
	for a := range attachments {
		names = append(names, a)
	}
	sort.Strings(names)
	for _, a := range names {
		src, err := os.Open(filepath.Join(s.Cfg.DataDir, "uploads", a))
		if err != nil {
			continue // referenced but missing on disk
		}
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: root + "/attachments/" + a, Method: zip.Deflate, Modified: time.Now()})
		if err == nil {
			_, _ = io.Copy(fw, src)
		}
		src.Close()
	}
	_ = zw.Close()
}
