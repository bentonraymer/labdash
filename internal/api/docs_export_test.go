package api

import "testing"

func TestPlanDocsExport(t *testing.T) {
	id := func(v int64) *int64 { return &v }
	folders := map[int64]exportFolder{
		1: {id: 1, name: "Runbooks"},
		2: {id: 2, parent: id(1), name: "Proxmox"},
	}
	notes := []exportNote{
		{id: 1, title: "Upgrade", folderID: id(2)},
		{id: 2, title: "Jellyfin", service: "Jellyfin"},
		{id: 3, title: "Transcoding", service: "Jellyfin"},
		{id: 4, title: "Ideas"},
		{id: 5, title: "Ideas"},
		{id: 6, title: "a/b: c?"},
		{id: 7, title: "Upgrade", folderID: id(2)},
	}
	got := planDocsExport(notes, folders)
	want := map[int64]string{
		1: "Runbooks/Proxmox/Upgrade.md",
		2: "Services/Jellyfin.md",
		3: "Services/Jellyfin - Transcoding.md",
		4: "Notes/Ideas.md",
		5: "Notes/Ideas (2).md",
		6: "Notes/a-b- c.md",
		7: "Runbooks/Proxmox/Upgrade (2).md",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("note %d: got %q want %q", k, got[k], v)
		}
	}
}

func TestRewriteUploads(t *testing.T) {
	body := "![shot](/media/uploads/0123456789abcdef01234567.png) and [file](/media/uploads/0123456789abcdef01234568.yml)"
	out, files := rewriteUploads(body, "Runbooks/Proxmox/Upgrade.md")
	want := "![shot](../../attachments/0123456789abcdef01234567.png) and [file](../../attachments/0123456789abcdef01234568.yml)"
	if out != want || len(files) != 2 {
		t.Fatalf("got %q %v", out, files)
	}
	if out, _ := rewriteUploads("![x](/media/uploads/0123456789abcdef01234567.png)", "Notes/x.md"); out != "![x](../attachments/0123456789abcdef01234567.png)" {
		t.Fatalf("depth 1: %q", out)
	}
}
