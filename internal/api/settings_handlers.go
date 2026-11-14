package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// settingSpec describes one GUI-editable setting: its default and a validator.
type settingSpec struct {
	def      any
	validate func(v any) error
}

var settingSpecs = map[string]settingSpec{
	"site_title":            {"LabDash", isString(1, 60)},
	"access_mode":           {"dns", oneOf("dns", "direct")},
	"dns_domain":            {"", isString(0, 253)},
	"wallpaper":             {"", isString(0, 500)},
	"wallpaper_blur":        {float64(0), isNumber(0, 40)},
	"wallpaper_dim":         {float64(35), isNumber(0, 95)},
	"alerts_paused":         {false, isBool},
	"new_device_alerts":     {true, isBool},
	"backup_auto":           {true, isBool},
	"backup_keep":           {float64(7), isNumber(1, 90)},
	"alert_group_seconds":   {float64(30), isNumber(0, 300)},
	"alert_max_per_hour":    {float64(20), isNumber(0, 500)},
	"alert_flap_protection": {true, isBool},
	"tile_glow":             {true, isBool},
	"open_new_tab":          {true, isBool},
	"vuln_enabled":          {true, isBool},
	"updates_digest":        {true, isBool},
	"todo_remind_time":      {"09:00", isClock},
	"vuln_alert_min":        {"high", oneOf("critical", "high", "medium", "low", "off")},
}

func (s *Server) loadSettings(ctx context.Context) (map[string]any, error) {
	out := make(map[string]any, len(settingSpecs))
	for k, spec := range settingSpecs {
		out[k] = spec.def
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, raw string
		if err := rows.Scan(&k, &raw); err != nil {
			return nil, err
		}
		if _, known := settingSpecs[k]; !known {
			continue
		}
		var v any
		if json.Unmarshal([]byte(raw), &v) == nil {
			out[k] = v
		}
	}
	return out, rows.Err()
}

func (s *Server) accessMode(ctx context.Context) string {
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'access_mode'`).Scan(&raw); err == nil {
		var m string
		if json.Unmarshal([]byte(raw), &m) == nil && m == "direct" {
			return "direct"
		}
	}
	return "dns"
}

func (s *Server) setSettings(ctx context.Context, patch map[string]any) error {
	for k, v := range patch {
		spec, ok := settingSpecs[k]
		if !ok {
			return fmt.Errorf("unknown setting %q", k)
		}
		if err := spec.validate(v); err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range patch {
		b, _ := json.Marshal(v)
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, string(b)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.loadSettings(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if err := readJSON(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.setSettings(r.Context(), patch); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := s.loadSettings(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	if _, ok := patch["access_mode"]; ok {
		s.reloadMonitors() // derived targets follow the access mode
	}
	s.Events.Publish("settings", st)
	writeJSON(w, http.StatusOK, st)
}

var wallpaperExt = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/avif": ".avif"}

func (s *Server) handleWallpaperUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
	f, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "expected multipart field 'file' (max 25 MB)")
		return
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	ext, ok := wallpaperExt[http.DetectContentType(head[:n])]
	if !ok {
		// DetectContentType doesn't know AVIF; accept by magic.
		if n > 12 && string(head[4:12]) == "ftypavif" {
			ext = ".avif"
		} else {
			writeErr(w, http.StatusBadRequest, "wallpaper must be JPEG, PNG, WebP or AVIF")
			return
		}
	}
	rnd := make([]byte, 8)
	_, _ = rand.Read(rnd)
	name := hex.EncodeToString(rnd) + ext
	dst, err := os.Create(filepath.Join(s.Cfg.DataDir, "wallpapers", name))
	if err != nil {
		serverErr(w, err)
		return
	}
	defer dst.Close()
	if _, err := dst.Write(head[:n]); err != nil {
		serverErr(w, err)
		return
	}
	if _, err := io.Copy(dst, f); err != nil {
		serverErr(w, err)
		return
	}
	url := "/media/wallpapers/" + name
	if err := s.setSettings(r.Context(), map[string]any{"wallpaper": url}); err != nil {
		serverErr(w, err)
		return
	}
	st, _ := s.loadSettings(r.Context())
	s.Events.Publish("settings", st)
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// ── validators ────────────────────────────────────────────────────────────

func isString(min, max int) func(any) error {
	return func(v any) error {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("must be a string")
		}
		if l := len(strings.TrimSpace(s)); l < min || len(s) > max {
			return fmt.Errorf("length must be %d–%d", min, max)
		}
		return nil
	}
}

func isNumber(min, max float64) func(any) error {
	return func(v any) error {
		f, ok := v.(float64)
		if !ok || f < min || f > max {
			return fmt.Errorf("must be a number %g–%g", min, max)
		}
		return nil
	}
}

func isBool(v any) error {
	if _, ok := v.(bool); !ok {
		return fmt.Errorf("must be true/false")
	}
	return nil
}

func oneOf(opts ...string) func(any) error {
	return func(v any) error {
		s, _ := v.(string)
		for _, o := range opts {
			if s == o {
				return nil
			}
		}
		return fmt.Errorf("must be one of %v", opts)
	}
}

var clockRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func isClock(v any) error {
	s, _ := v.(string)
	if !clockRe.MatchString(s) {
		return fmt.Errorf("use a 24-hour time like 09:00")
	}
	return nil
}
