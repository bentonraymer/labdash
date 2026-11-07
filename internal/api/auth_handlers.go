package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/bentonraymer/labdash/internal/auth"
)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	need, err := s.Auth.NeedsSetup(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"needsSetup": need, "version": s.Version})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		credentials
		DNSDomain string `json:"dnsDomain"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.Auth.CreateFirstUser(r.Context(), body.Username, body.Password)
	if errors.Is(err, auth.ErrSetupDone) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.DNSDomain != "" {
		_ = s.setSettings(r.Context(), map[string]any{"dns_domain": body.DNSDomain})
	}
	tok, err := s.Auth.NewSession(r.Context(), u.ID, r.UserAgent())
	if err != nil {
		serverErr(w, err)
		return
	}
	auth.SetCookie(w, r, tok)
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ip := r.RemoteAddr
	if h, _, err := net.SplitHostPort(ip); err == nil {
		ip = h
	}
	tok, u, err := s.Auth.Login(r.Context(), ip, c.Username, c.Password, r.UserAgent())
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		writeErr(w, http.StatusTooManyRequests, err.Error())
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		time.Sleep(300 * time.Millisecond)
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	case err != nil:
		serverErr(w, err)
		return
	}
	auth.SetCookie(w, r, tok)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.Auth.Logout(r.Context(), c.Value)
	}
	auth.ClearCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, auth.UserFrom(r.Context()))
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Auth.ChangePassword(r.Context(), auth.UserFrom(r.Context()).ID, body.Current, body.Next); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleEvents streams live updates (status changes, settings, scan progress).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.Events.Subscribe()
	defer cancel()
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
