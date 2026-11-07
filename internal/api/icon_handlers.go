package api

import (
	"context"
	"io"
	"net/http"
	"time"
)

// handleIconIndex returns the searchable dashboard-icons index.
func (s *Server) handleIconIndex(w http.ResponseWriter, r *http.Request) {
	b, err := s.Icons.Index(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "couldn't load icon index: "+err.Error())
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	w.Write(b)
}

// handleIconUpload stores an uploaded image as a custom icon.
func (s *Server) handleIconUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	f, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "expected multipart field 'file' (max 2 MB)")
		return
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	url, err := s.Icons.SaveCustom(b)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"icon": url})
}

// handleIconFavicon fetches a service's own favicon and stores it.
func (s *Server) handleIconFavicon(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	url, err := s.Icons.FetchFavicon(ctx, body.URL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "couldn't fetch favicon: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"icon": url})
}
