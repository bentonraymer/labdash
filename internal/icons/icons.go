// Package icons serves service icons. Names from the dashboard-icons project
// ("di:jellyfin") are fetched once from the CDN and cached under /data/icons.
package icons

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const cdnBase = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons"

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,80}$`)

type Cache struct {
	dir    string
	client *http.Client
	mu     sync.Mutex
	inFlt  map[string]*sync.Mutex
}

func New(dir string) *Cache {
	return &Cache{dir: dir, client: &http.Client{Timeout: 15 * time.Second}, inFlt: map[string]*sync.Mutex{}}
}

var mimeByExt = map[string]string{"svg": "image/svg+xml", "png": "image/png", "webp": "image/webp"}

// ServeHTTP handles /icons/di/{name}.{ext}.
func (c *Cache) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	file := strings.TrimPrefix(r.URL.Path, "/icons/di/")
	ext := strings.TrimPrefix(filepath.Ext(file), ".")
	name := strings.TrimSuffix(file, "."+ext)
	mime, ok := mimeByExt[ext]
	if !ok || !nameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	path, err := c.ensure(r.Context(), name, ext)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=604800")
	if ext == "svg" {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	}
	http.ServeFile(w, r, path)
}

func (c *Cache) ensure(ctx context.Context, name, ext string) (string, error) {
	path := filepath.Join(c.dir, "di", name+"."+ext)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	key := name + "." + ext
	c.mu.Lock()
	m, ok := c.inFlt[key]
	if !ok {
		m = &sync.Mutex{}
		c.inFlt[key] = m
	}
	c.mu.Unlock()
	m.Lock()
	defer m.Unlock()
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	url := fmt.Sprintf("%s/%s/%s.%s", cdnBase, ext, name, ext)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("icon %s not found", key)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 2<<20)); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	f.Close()
	return path, os.Rename(tmp, path)
}
