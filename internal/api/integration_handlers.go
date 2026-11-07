package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bentonraymer/labdash/internal/integrations"
	"github.com/bentonraymer/labdash/internal/pushover"
)

func hint(s string) string {
	if len(s) <= 4 {
		return ""
	}
	return "…" + s[len(s)-4:]
}

func (s *Server) handleGetPushover(w http.ResponseWriter, r *http.Request) {
	var sec pushover.Secret
	var cfg pushover.Config
	enabled, err := s.Integrations.Get(r.Context(), "pushover", &cfg, &sec)
	if err != nil && !errors.Is(err, integrations.ErrNotConfigured) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured":   sec.UserKey != "" && sec.AppToken != "",
		"enabled":      enabled,
		"device":       cfg.Device,
		"baseUrl":      cfg.BaseURL,
		"userKeyHint":  hint(sec.UserKey),
		"appTokenHint": hint(sec.AppToken),
	})
}

func (s *Server) handlePutPushover(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled  bool   `json:"enabled"`
		Device   string `json:"device"`
		BaseURL  string `json:"baseUrl"`
		UserKey  string `json:"userKey"`  // '' keeps the stored value
		AppToken string `json:"appToken"` // '' keeps the stored value
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	var sec pushover.Secret
	_, err := s.Integrations.Get(ctx, "pushover", nil, &sec)
	if err != nil && !errors.Is(err, integrations.ErrNotConfigured) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v := strings.TrimSpace(body.UserKey); v != "" {
		sec.UserKey = v
	}
	if v := strings.TrimSpace(body.AppToken); v != "" {
		sec.AppToken = v
	}
	cfg := pushover.Config{Device: strings.TrimSpace(body.Device), BaseURL: strings.TrimSpace(body.BaseURL)}
	if err := s.Integrations.Put(ctx, "pushover", body.Enabled, cfg, sec); err != nil {
		serverErr(w, err)
		return
	}
	s.handleGetPushover(w, r)
}

func (s *Server) handleTestPushover(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var sec pushover.Secret
	var cfg pushover.Config
	if _, err := s.Integrations.Get(ctx, "pushover", &cfg, &sec); err != nil {
		writeErr(w, http.StatusBadRequest, "save your Pushover keys first")
		return
	}
	msg := pushover.Message{Title: "LabDash test", Body: "✅ Pushover is connected. You'll get alerts here when services go down."}
	if cfg.BaseURL != "" {
		msg.URL = strings.TrimRight(cfg.BaseURL, "/") + "/status"
		msg.URLTitle = "Open LabDash status"
	}
	if err := s.Pushover.Send(ctx, sec, cfg, msg); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
