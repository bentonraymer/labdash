package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Version snapshots are taken at most this often while a note is edited.
const versionEvery = 5 * time.Minute

const maxVersionsPerNote = 200

// ── folders ───────────────────────────────────────────────────────────────

type Folder struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parentId"`
	Name      string `json:"name"`
	Position  int    `json:"position"`
	NoteCount int    `json:"noteCount"`
}

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT f.id, f.parent_id, f.name, f.position,
		(SELECT COUNT(*) FROM notes n WHERE n.folder_id = f.id) FROM folders f ORDER BY f.position, f.name COLLATE NOCASE`)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var f Folder
		var p sql.NullInt64
		if err := rows.Scan(&f.ID, &p, &f.Name, &f.Position, &f.NoteCount); err != nil {
			serverErr(w, err)
			return
		}
		if p.Valid {
			f.ParentID = &p.Int64
		}
		out = append(out, f)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSaveFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        *string `json:"name"`
		ParentID    *int64  `json:"parentId"`
		ClearParent bool    `json:"clearParent"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if r.Method == http.MethodPost {
		if in.Name == nil {
			writeErr(w, http.StatusBadRequest, "name is required")
			return
		}
		res, err := s.DB.ExecContext(r.Context(), `INSERT INTO folders(name, parent_id, position) VALUES (?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM folders))`,
			strings.TrimSpace(*in.Name), in.ParentID)
		if err != nil {
			serverErr(w, err)
			return
		}
		id, _ := res.LastInsertId()
		writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.ParentID != nil && *in.ParentID == id {
		writeErr(w, http.StatusBadRequest, "a folder can't contain itself")
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE folders SET name = COALESCE(?, name),
		parent_id = CASE WHEN ? THEN NULL ELSE COALESCE(?, parent_id) END WHERE id = ?`, trimPtr(in.Name), in.ClearParent, in.ParentID, id); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Subfolders cascade; notes inside become unfiled (never deleted).
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM folders WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── notes ─────────────────────────────────────────────────────────────────

type NoteSummary struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	FolderID    *int64   `json:"folderId"`
	ServiceID   *int64   `json:"serviceId"`
	ServiceName string   `json:"serviceName"`
	ServiceIcon string   `json:"serviceIcon"`
	Pinned      bool     `json:"pinned"`
	Tags        []string `json:"tags"`
	UpdatedAt   string   `json:"updatedAt"`
	Snippet     string   `json:"snippet"`
}

type Note struct {
	NoteSummary
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

var mdNoise = regexp.MustCompile("[#*_`>\\[\\]()!|-]+")

func snippetOf(body string) string {
	s := strings.Join(strings.Fields(mdNoise.ReplaceAllString(body, " ")), " ")
	if len(s) > 140 {
		s = s[:140] + "…"
	}
	return s
}

func (s *Server) noteTags(ctx context.Context) (map[int64][]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT note_id, tag FROM note_tags ORDER BY tag COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var t string
		if rows.Scan(&id, &t) == nil {
			out[id] = append(out[id], t)
		}
	}
	return out, rows.Err()
}

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.DB.QueryContext(ctx, `SELECT n.id, n.title, n.folder_id, n.service_id, COALESCE(sv.name, ''), COALESCE(sv.icon, ''),
		n.pinned, n.updated_at, substr(n.body, 1, 600)
		FROM notes n LEFT JOIN services sv ON sv.id = n.service_id ORDER BY n.pinned DESC, n.updated_at DESC`)
	if err != nil {
		serverErr(w, err)
		return
	}
	var out []NoteSummary
	for rows.Next() {
		var n NoteSummary
		var f, sv sql.NullInt64
		var body string
		if err := rows.Scan(&n.ID, &n.Title, &f, &sv, &n.ServiceName, &n.ServiceIcon, &n.Pinned, &n.UpdatedAt, &body); err != nil {
			rows.Close()
			serverErr(w, err)
			return
		}
		if f.Valid {
			n.FolderID = &f.Int64
		}
		if sv.Valid {
			n.ServiceID = &sv.Int64
		}
		n.Snippet = snippetOf(body)
		out = append(out, n)
	}
	rows.Close()
	tags, err := s.noteTags(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	for i := range out {
		out[i].Tags = tags[out[i].ID]
		if out[i].Tags == nil {
			out[i].Tags = []string{}
		}
	}
	if out == nil {
		out = []NoteSummary{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getNote(ctx context.Context, id int64) (*Note, error) {
	var n Note
	var f, sv sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT n.id, n.title, n.folder_id, n.service_id, COALESCE(sv.name, ''), COALESCE(sv.icon, ''),
		n.pinned, n.updated_at, n.created_at, n.body FROM notes n LEFT JOIN services sv ON sv.id = n.service_id WHERE n.id = ?`, id).
		Scan(&n.ID, &n.Title, &f, &sv, &n.ServiceName, &n.ServiceIcon, &n.Pinned, &n.UpdatedAt, &n.CreatedAt, &n.Body)
	if err != nil {
		return nil, err
	}
	if f.Valid {
		n.FolderID = &f.Int64
	}
	if sv.Valid {
		n.ServiceID = &sv.Int64
	}
	n.Tags = []string{}
	rows, err := s.DB.QueryContext(ctx, `SELECT tag FROM note_tags WHERE note_id = ? ORDER BY tag COLLATE NOCASE`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			n.Tags = append(n.Tags, t)
		}
	}
	return &n, nil
}

func (s *Server) handleGetNote(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := s.getNote(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "note not found")
		return
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

type noteInput struct {
	Title        *string   `json:"title"`
	Body         *string   `json:"body"`
	FolderID     *int64    `json:"folderId"`
	ClearFolder  bool      `json:"clearFolder"`
	ServiceID    *int64    `json:"serviceId"`
	ClearService bool      `json:"clearService"`
	Pinned       *bool     `json:"pinned"`
	Tags         *[]string `json:"tags"`
	// BaseUpdatedAt is the updatedAt the client last saw. If the note changed
	// since (e.g. edited in another tab), the save is rejected with 409.
	BaseUpdatedAt string `json:"baseUpdatedAt"`
	Force         bool   `json:"force"`
}

func normalizeTags(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range in {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		if t == "" || len(t) > 40 || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
	}
	return out
}

func setTags(ctx context.Context, tx *sql.Tx, noteID int64, tags []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id = ?`, noteID); err != nil {
		return err
	}
	for _, t := range normalizeTags(tags) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO note_tags(note_id, tag) VALUES (?, ?)`, noteID, t); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	var in noteInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	title := strings.TrimSpace(deref(in.Title, ""))
	if title == "" {
		title = "Untitled"
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer tx.Rollback()
	ts := now()
	res, err := tx.ExecContext(ctx, `INSERT INTO notes(title, body, folder_id, service_id, pinned, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		title, deref(in.Body, ""), in.FolderID, in.ServiceID, deref(in.Pinned, false), ts, ts)
	if err != nil {
		serverErr(w, err)
		return
	}
	id, _ := res.LastInsertId()
	if in.Tags != nil {
		if err := setTags(ctx, tx, id, *in.Tags); err != nil {
			serverErr(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		serverErr(w, err)
		return
	}
	n, err := s.getNote(ctx, id)
	if err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("notes", map[string]int64{"id": id})
	writeJSON(w, http.StatusCreated, n)
}

// handleUpdateNote is the autosave endpoint. It snapshots a version at most
// every versionEvery while the body keeps changing.
func (s *Server) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in noteInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer tx.Rollback()

	var curTitle, curBody, updatedAt string
	if err := tx.QueryRowContext(ctx, `SELECT title, body, updated_at FROM notes WHERE id = ?`, id).Scan(&curTitle, &curBody, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "note not found")
			return
		}
		serverErr(w, err)
		return
	}
	if in.BaseUpdatedAt != "" && in.BaseUpdatedAt != updatedAt && !in.Force {
		tx.Rollback()
		n, _ := s.getNote(ctx, id)
		writeJSON(w, http.StatusConflict, map[string]any{"error": "this note was changed somewhere else", "note": n})
		return
	}

	var title *string
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			t = "Untitled"
		}
		title = &t
	}
	ts := now()
	if _, err := tx.ExecContext(ctx, `UPDATE notes SET title = COALESCE(?, title), body = COALESCE(?, body),
		folder_id = CASE WHEN ? THEN NULL ELSE COALESCE(?, folder_id) END,
		service_id = CASE WHEN ? THEN NULL ELSE COALESCE(?, service_id) END,
		pinned = COALESCE(?, pinned), updated_at = ? WHERE id = ?`,
		title, in.Body, in.ClearFolder, in.FolderID, in.ClearService, in.ServiceID, in.Pinned, ts, id); err != nil {
		serverErr(w, err)
		return
	}
	if in.Tags != nil {
		if err := setTags(ctx, tx, id, *in.Tags); err != nil {
			serverErr(w, err)
			return
		}
	}
	if in.Body != nil && *in.Body != curBody {
		var last sql.NullString
		_ = tx.QueryRowContext(ctx, `SELECT MAX(created_at) FROM note_versions WHERE note_id = ?`, id).Scan(&last)
		lastT, _ := time.Parse("2006-01-02T15:04:05.000Z", last.String)
		if !last.Valid || time.Since(lastT) >= versionEvery {
			newTitle := curTitle
			if title != nil {
				newTitle = *title
			}
			if err := snapshot(ctx, tx, id, newTitle, *in.Body); err != nil {
				serverErr(w, err)
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("notes", map[string]int64{"id": id})
	writeJSON(w, http.StatusOK, map[string]string{"updatedAt": ts})
}

func snapshot(ctx context.Context, tx *sql.Tx, noteID int64, title, body string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO note_versions(note_id, title, body, created_at) VALUES (?, ?, ?, ?)`, noteID, title, body, now()); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM note_versions WHERE note_id = ? AND id NOT IN
		(SELECT id FROM note_versions WHERE note_id = ? ORDER BY id DESC LIMIT ?)`, noteID, noteID, maxVersionsPerNote)
	return err
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM notes WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("notes", map[string]int64{"id": id})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT tag, COUNT(*) FROM note_tags GROUP BY lower(tag) ORDER BY tag COLLATE NOCASE`)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer rows.Close()
	type tag struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	out := []tag{}
	for rows.Next() {
		var t tag
		if rows.Scan(&t.Tag, &t.Count) == nil {
			out = append(out, t)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ── versions ──────────────────────────────────────────────────────────────

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id, title, created_at, length(body) FROM note_versions WHERE note_id = ? ORDER BY id DESC`, id)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer rows.Close()
	type ver struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		CreatedAt string `json:"createdAt"`
		Size      int    `json:"size"`
	}
	out := []ver{}
	for rows.Next() {
		var v ver
		if rows.Scan(&v.ID, &v.Title, &v.CreatedAt, &v.Size) == nil {
			out = append(out, v)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	vid, _ := strconv.ParseInt(chi.URLParam(r, "vid"), 10, 64)
	var title, body, created string
	if err := s.DB.QueryRowContext(r.Context(), `SELECT title, body, created_at FROM note_versions WHERE id = ? AND note_id = ?`, vid, id).Scan(&title, &body, &created); err != nil {
		writeErr(w, http.StatusNotFound, "version not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": vid, "title": title, "body": body, "createdAt": created})
}

// handleSnapshot records the current content as a version if it differs
// from the latest one (called when the editor loses focus or closes).
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer tx.Rollback()
	var title, body string
	if err := tx.QueryRowContext(ctx, `SELECT title, body FROM notes WHERE id = ?`, id).Scan(&title, &body); err != nil {
		writeErr(w, http.StatusNotFound, "note not found")
		return
	}
	var lastBody sql.NullString
	_ = tx.QueryRowContext(ctx, `SELECT body FROM note_versions WHERE note_id = ? ORDER BY id DESC LIMIT 1`, id).Scan(&lastBody)
	created := false
	if strings.TrimSpace(body) != "" && (!lastBody.Valid || lastBody.String != body) {
		if err := snapshot(ctx, tx, id, title, body); err != nil {
			serverErr(w, err)
			return
		}
		created = true
	}
	if err := tx.Commit(); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"created": created})
}

func (s *Server) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	vid, _ := strconv.ParseInt(chi.URLParam(r, "vid"), 10, 64)
	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer tx.Rollback()
	var vTitle, vBody, curTitle, curBody string
	if err := tx.QueryRowContext(ctx, `SELECT title, body FROM note_versions WHERE id = ? AND note_id = ?`, vid, id).Scan(&vTitle, &vBody); err != nil {
		writeErr(w, http.StatusNotFound, "version not found")
		return
	}
	if err := tx.QueryRowContext(ctx, `SELECT title, body FROM notes WHERE id = ?`, id).Scan(&curTitle, &curBody); err != nil {
		writeErr(w, http.StatusNotFound, "note not found")
		return
	}
	// Keep what's being replaced, so a restore can itself be undone
	// (unless the latest version already holds exactly this text).
	var lastBody sql.NullString
	_ = tx.QueryRowContext(ctx, `SELECT body FROM note_versions WHERE note_id = ? ORDER BY id DESC LIMIT 1`, id).Scan(&lastBody)
	if !lastBody.Valid || lastBody.String != curBody {
		if err := snapshot(ctx, tx, id, curTitle, curBody); err != nil {
			serverErr(w, err)
			return
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notes SET title = ?, body = ?, updated_at = ? WHERE id = ?`, vTitle, vBody, now(), id); err != nil {
		serverErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		serverErr(w, err)
		return
	}
	n, err := s.getNote(ctx, id)
	if err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("notes", map[string]int64{"id": id})
	writeJSON(w, http.StatusOK, n)
}

// ── search ────────────────────────────────────────────────────────────────

var ftsToken = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// ftsQuery turns free text into a safe FTS5 prefix query: "jelly conf" →
// "jelly"* "conf"* (all tokens must match).
func ftsQuery(q string) string {
	toks := ftsToken.FindAllString(q, 12)
	for i, t := range toks {
		toks[i] = `"` + t + `"*`
	}
	return strings.Join(toks, " ")
}

func (s *Server) handleSearchNotes(w http.ResponseWriter, r *http.Request) {
	q := ftsQuery(r.URL.Query().Get("q"))
	type hit struct {
		ID          int64   `json:"id"`
		Title       string  `json:"title"`
		Snippet     string  `json:"snippet"`
		ServiceName string  `json:"serviceName"`
		Rank        float64 `json:"rank"`
	}
	out := []hit{}
	if q == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT n.id, n.title, snippet(notes_fts, 1, '[[', ']]', '…', 12), COALESCE(sv.name, ''), bm25(notes_fts, 8.0, 1.0)
		FROM notes_fts JOIN notes n ON n.id = notes_fts.rowid LEFT JOIN services sv ON sv.id = n.service_id
		WHERE notes_fts MATCH ? ORDER BY bm25(notes_fts, 8.0, 1.0) LIMIT 20`, q)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var h hit
		if rows.Scan(&h.ID, &h.Title, &h.Snippet, &h.ServiceName, &h.Rank) == nil {
			out = append(out, h)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ── attachments ───────────────────────────────────────────────────────────

const maxAttachment = 25 << 20

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachment+1<<20)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "expected multipart field 'file' (max 25 MB)")
		return
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	mimeType := http.DetectContentType(head[:n])
	if i := strings.IndexByte(mimeType, ';'); i > 0 {
		mimeType = mimeType[:i]
	}
	name := filepath.Base(strings.ReplaceAll(hdr.Filename, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".svg" {
		mimeType = "image/svg+xml"
	}
	if len(ext) > 10 || !regexp.MustCompile(`^\.[a-z0-9]*$`).MatchString(ext) {
		ext = ""
	}
	rnd := make([]byte, 12)
	_, _ = rand.Read(rnd)
	stored := hex.EncodeToString(rnd) + ext
	dst, err := os.Create(filepath.Join(s.Cfg.DataDir, "uploads", stored))
	if err != nil {
		serverErr(w, err)
		return
	}
	defer dst.Close()
	if _, err := dst.Write(head[:n]); err != nil {
		serverErr(w, err)
		return
	}
	size, err := io.Copy(dst, f)
	if err != nil {
		os.Remove(dst.Name())
		writeErr(w, http.StatusBadRequest, "upload failed (max 25 MB)")
		return
	}
	size += int64(n)
	if _, err := s.DB.ExecContext(r.Context(), `INSERT INTO attachments(note_id, filename, mime, size, path) VALUES (?, ?, ?, ?, ?)`,
		id, name, mimeType, size, stored); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"url": "/media/uploads/" + stored, "filename": name, "mime": mimeType, "size": size,
		"isImage": strings.HasPrefix(mimeType, "image/"),
	})
}

var uploadNameRe = regexp.MustCompile(`^[a-f0-9]{24}(\.[a-z0-9]{1,9})?$`)

// handleServeUpload serves note attachments (auth required). Images render
// inline; everything else downloads with its original filename.
func (s *Server) handleServeUpload(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "*")
	if !uploadNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	var filename, mimeType string
	if err := s.DB.QueryRowContext(r.Context(), `SELECT filename, mime FROM attachments WHERE path = ?`, name).Scan(&filename, &mimeType); err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", mimeType)
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox")
	disp := "attachment"
	if strings.HasPrefix(mimeType, "image/") || mimeType == "application/pdf" {
		disp = "inline"
	}
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": filename}))
	http.ServeFile(w, r, filepath.Join(s.Cfg.DataDir, "uploads", name))
}
