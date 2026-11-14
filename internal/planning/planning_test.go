package planning

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bentonraymer/labdash/internal/db"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNextCalendar(t *testing.T) {
	cases := []struct {
		anchor, after string
		every         Every
		want          string
	}{
		{"2026-01-01 09:00", "2026-11-14 10:00", Every{1, "quarter"}, "2027-01-01 09:00"},
		{"2026-01-01 09:00", "2026-09-30 10:00", Every{1, "quarter"}, "2026-10-01 09:00"},
		{"2026-01-31 09:00", "2026-02-01 00:00", Every{1, "month"}, "2026-02-28 09:00"}, // clamped
		{"2026-01-31 09:00", "2026-03-01 00:00", Every{1, "month"}, "2026-03-31 09:00"}, // no drift
		{"2026-11-01 08:00", "2026-11-14 12:00", Every{2, "week"}, "2026-11-15 08:00"},
		{"2026-12-01 09:00", "2026-11-14 12:00", Every{1, "year"}, "2026-12-01 09:00"}, // anchor still ahead
		{"2024-02-29 09:00", "2025-01-01 00:00", Every{1, "year"}, "2025-02-28 09:00"},
	}
	for _, c := range cases {
		got := NextCalendar(at(c.anchor), c.every, at(c.after))
		if !got.Equal(at(c.want)) {
			t.Errorf("%s every %v after %s: got %s want %s", c.anchor, c.every, c.after, got.Format("2006-01-02 15:04"), c.want)
		}
	}
}

func TestNextAfterDone(t *testing.T) {
	got := NextAfterDone(at("2026-01-01 09:00"), Every{3, "month"}, at("2026-11-14 22:30"))
	if !got.Equal(at("2027-02-14 09:00")) {
		t.Fatalf("got %s", got)
	}
}

func TestDescribe(t *testing.T) {
	for _, c := range []struct {
		kind string
		e    Every
		want string
	}{{"calendar", Every{1, "quarter"}, "Quarterly"}, {"calendar", Every{2, "week"}, "Every 2 weeks"}, {"after_done", Every{6, "month"}, "6 months after done"}, {"once", Every{1, "day"}, "Once"}} {
		if got := Describe(c.kind, c.e); got != c.want {
			t.Errorf("%v: %q", c, got)
		}
	}
}

func TestTickAndComplete(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	now := at("2026-10-01 09:00")
	s := New(d, nil)
	s.Now = func() time.Time { return now }
	var sent []Message
	paused := false
	s.Send = func(_ context.Context, m Message) bool {
		if paused {
			return false
		}
		sent = append(sent, m)
		return true
	}
	ts := func(t time.Time) string { return t.UTC().Format(TS) }
	d.Exec(`INSERT INTO reminders(id, title, kind, every_n, unit, anchor_at, due_at) VALUES (1, 'Check backups', 'calendar', 1, 'quarter', ?, ?)`, ts(at("2026-01-01 09:00")), ts(at("2026-10-01 09:00")))
	d.Exec(`INSERT INTO reminders(id, title, kind, anchor_at, due_at) VALUES (2, 'Renew domain', 'once', ?, ?)`, ts(at("2026-12-01 09:00")), ts(at("2026-12-01 09:00")))
	d.Exec(`INSERT INTO todos(id, title, priority, due_date, remind) VALUES (1, 'Replace UPS battery', 1, '2026-10-01', 1), (2, 'No reminder', 2, '2026-09-01', 0)`)

	s.Tick(ctx)
	if len(sent) != 2 || sent[0].Title != "⏰ Check backups" || sent[1].Title != "📋 Todo due: Replace UPS battery" {
		t.Fatalf("first tick: %+v", sent)
	}
	// Same minute / a few hours later: nothing new.
	now = now.Add(5 * time.Hour)
	s.Tick(ctx)
	if len(sent) != 2 {
		t.Fatalf("re-sent too early: %d", len(sent))
	}
	// Next day: daily nag for both, todo now overdue.
	now = at("2026-10-02 09:01")
	s.Tick(ctx)
	if len(sent) != 4 || sent[3].Title != "📋 Todo overdue: Replace UPS battery" {
		t.Fatalf("nag: %+v", sent[2:])
	}
	// Snooze holds it; when it ends it sends once.
	s.Snooze(ctx, 1, now.Add(2*time.Hour))
	now = now.Add(time.Hour)
	s.Tick(ctx)
	paused = true
	now = now.Add(2 * time.Hour)
	s.Tick(ctx) // paused: nothing recorded
	paused = false
	s.Tick(ctx)
	if len(sent) != 5 || sent[4].Title != "⏰ Check backups" {
		t.Fatalf("after snooze: %+v", sent[4:])
	}
	// Done: next quarter, nag state cleared.
	if err := s.Complete(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	var due string
	var last *string
	d.QueryRow(`SELECT due_at, last_sent_at FROM reminders WHERE id = 1`).Scan(&due, &last)
	if due != ts(at("2027-01-01 09:00")) || last != nil {
		t.Fatalf("after done: due %s last %v", due, last)
	}
	// Completing a once reminder disables it.
	s.Complete(ctx, 2, false)
	var enabled bool
	d.QueryRow(`SELECT enabled FROM reminders WHERE id = 2`).Scan(&enabled)
	if enabled {
		t.Fatal("once reminder still enabled after done")
	}
}
