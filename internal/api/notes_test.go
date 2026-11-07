package api

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bentonraymer/labdash/internal/db"
)

func TestFTSQuery(t *testing.T) {
	cases := map[string]string{
		"jelly conf":         `"jelly"* "conf"*`,
		`"; DROP TABLE x --`: `"DROP"* "TABLE"* "x"*`,
		"  ":                 "",
		"docker-compose.yml": `"docker"* "compose"* "yml"*`,
	}
	for in, want := range cases {
		if got := ftsQuery(in); got != want {
			t.Errorf("ftsQuery(%q) = %q want %q", in, got, want)
		}
	}
}

func TestNormalizeTags(t *testing.T) {
	got := normalizeTags([]string{" #media ", "Media", "", "arr", "#"})
	if !reflect.DeepEqual(got, []string{"media", "arr"}) {
		t.Fatalf("got %v", got)
	}
}

// The FTS index follows inserts, updates and deletes via triggers.
func TestNotesFTS(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO notes(id, title, body) VALUES (1, 'Jellyfin', 'Hardware transcoding uses the iGPU via /dev/dri')`)
	d.Exec(`INSERT INTO notes(id, title, body) VALUES (2, 'Router', 'Static leases live in /etc/config/dhcp')`)
	count := func(q string) int {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM notes_fts WHERE notes_fts MATCH ?`, ftsQuery(q)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count("transcod") != 1 || count("jelly") != 1 || count("dhcp leases") != 1 || count("nothing") != 0 {
		t.Fatal("unexpected search results")
	}
	d.Exec(`UPDATE notes SET body = 'QuickSync now' WHERE id = 1`)
	if count("transcod") != 0 || count("quicksync") != 1 {
		t.Fatal("index not updated")
	}
	d.Exec(`DELETE FROM notes WHERE id = 2`)
	if count("dhcp") != 0 {
		t.Fatal("index not cleaned up on delete")
	}
}
