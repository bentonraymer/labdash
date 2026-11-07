package icons

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one icon in the searchable dashboard-icons index.
type Entry struct {
	Name    string   `json:"n"`
	Format  string   `json:"f"` // best available: svg > png > webp
	Aliases []string `json:"a,omitempty"`
	Cats    []string `json:"c,omitempty"`
}

const indexTTL = 7 * 24 * time.Hour

// Index returns the merged icon index, building it from the CDN's tree.json and
// metadata.json and caching the result on disk for a week. If the CDN is
// unreachable a stale cache is served.
func (c *Cache) Index(ctx context.Context) ([]byte, error) {
	path := filepath.Join(c.dir, "index.json")
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < indexTTL {
		return os.ReadFile(path)
	}
	c.idxMu.Lock()
	defer c.idxMu.Unlock()
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < indexTTL {
		return os.ReadFile(path)
	}
	b, err := c.buildIndex(ctx)
	if err != nil {
		if stale, serr := os.ReadFile(path); serr == nil {
			return stale, nil
		}
		return nil, err
	}
	_ = os.WriteFile(path, b, 0o640)
	return b, nil
}

func (c *Cache) fetchJSON(ctx context.Context, url string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(v)
}

func (c *Cache) buildIndex(ctx context.Context) ([]byte, error) {
	var tree map[string][]string
	if err := c.fetchJSON(ctx, cdnBase+"/tree.json", &tree); err != nil {
		return nil, err
	}
	var meta map[string]struct {
		Aliases    []string `json:"aliases"`
		Categories []string `json:"categories"`
	}
	_ = c.fetchJSON(ctx, cdnBase+"/metadata.json", &meta) // aliases are a nice-to-have

	formats := map[string]map[string]bool{}
	for _, f := range []string{"svg", "png", "webp"} {
		for _, file := range tree[f] {
			name := strings.TrimSuffix(file, "."+f)
			if formats[name] == nil {
				formats[name] = map[string]bool{}
			}
			formats[name][f] = true
		}
	}
	out := make([]Entry, 0, len(formats))
	for name, fs := range formats {
		e := Entry{Name: name, Format: "webp"}
		if fs["svg"] {
			e.Format = "svg"
		} else if fs["png"] {
			e.Format = "png"
		}
		// Variants like "foo-dark" inherit the base icon's metadata.
		base := strings.TrimSuffix(strings.TrimSuffix(name, "-dark"), "-light")
		if m, ok := meta[base]; ok {
			e.Aliases = m.Aliases
			e.Cats = m.Categories
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return json.Marshal(out)
}
