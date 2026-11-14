package planning

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bentonraymer/labdash/internal/events"
)

// NagEvery is how often an unfinished reminder is re-sent.
const NagEvery = 24 * time.Hour

// Message is one notification to deliver.
type Message struct {
	Title, Body, Path, LinkTitle string
}

// Service sends due reminders and todo due-date reminders.
type Service struct {
	DB  *sql.DB
	Hub *events.Hub
	// Send delivers a notification; it returns false when alerts are paused
	// (nothing is recorded, so it goes out once they resume).
	Send func(ctx context.Context, m Message) bool
	// TodoTime is the local time of day todo reminders go out ("09:00").
	TodoTime func(ctx context.Context) string
	Now      func() time.Time
}

func New(db *sql.DB, hub *events.Hub) *Service {
	return &Service{DB: db, Hub: hub, Now: time.Now}
}

func (s *Service) publish() {
	if s.Hub != nil {
		s.Hub.Publish("planning", map[string]bool{"changed": true})
	}
}

// Run checks every minute.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("reminders tick failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// shouldSend: first time for this occurrence (or after a snooze ends),
// then once per NagEvery until done.
func shouldSend(now, due time.Time, snoozed, lastSent *time.Time) bool {
	if now.Before(due) {
		return false
	}
	if snoozed != nil && now.Before(*snoozed) {
		return false
	}
	if lastSent == nil {
		return true
	}
	since := due
	if snoozed != nil && snoozed.After(since) {
		since = *snoozed
	}
	return lastSent.Before(since) || now.Sub(*lastSent) >= NagEvery
}

func parseTS(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(TS, s.String)
	if err != nil {
		return nil
	}
	return &t
}

// Tick sends whatever is due now.
func (s *Service) Tick(ctx context.Context) error {
	if s.Send == nil {
		return nil
	}
	now := s.Now().UTC()
	sent := 0
	n, err := s.tickReminders(ctx, now)
	if err != nil {
		return err
	}
	sent += n
	n, err = s.tickTodos(ctx, now)
	if err != nil {
		return err
	}
	if sent+n > 0 {
		s.publish()
	}
	return nil
}

func (s *Service) tickReminders(ctx context.Context, now time.Time) (int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, title, notes, kind, due_at, snoozed_until, last_sent_at FROM reminders
		WHERE enabled = 1 AND due_at <= ?`, now.Format(TS))
	if err != nil {
		return 0, err
	}
	type due struct {
		id                 int64
		title, notes, kind string
		due                time.Time
	}
	var list []due
	for rows.Next() {
		var d due
		var dueS string
		var snz, last sql.NullString
		if rows.Scan(&d.id, &d.title, &d.notes, &d.kind, &dueS, &snz, &last) != nil {
			continue
		}
		t, err := time.Parse(TS, dueS)
		if err != nil {
			continue
		}
		d.due = t
		if shouldSend(now, t, parseTS(snz), parseTS(last)) {
			list = append(list, d)
		}
	}
	rows.Close()
	count := 0
	for _, d := range list {
		body := strings.TrimSpace(d.notes)
		if r := []rune(body); len(r) > 600 {
			body = string(r[:600]) + "…"
		}
		if late := now.Sub(d.due); late > 20*time.Hour {
			if body != "" {
				body += "\n\n"
			}
			body += "Overdue since " + d.due.Local().Format("Mon Jan 2")
		}
		if !s.Send(ctx, Message{Title: "⏰ " + d.title, Body: body, Path: fmt.Sprintf("/reminders?r=%d", d.id), LinkTitle: "Mark done"}) {
			return count, nil // alerts paused
		}
		ts := now.Format(TS)
		if _, err := s.DB.ExecContext(ctx, `UPDATE reminders SET last_sent_at = ? WHERE id = ?`, ts, d.id); err != nil {
			return count, err
		}
		_, _ = s.DB.ExecContext(ctx, `INSERT INTO reminder_events(reminder_id, ts, kind, due_at) VALUES (?, ?, 'sent', ?)`, d.id, ts, d.due.Format(TS))
		count++
	}
	return count, nil
}

// todoDue is when a todo's reminder goes out: its due date at TodoTime.
func (s *Service) todoDue(ctx context.Context, date string) (time.Time, error) {
	at := "09:00"
	if s.TodoTime != nil {
		if v := s.TodoTime(ctx); v != "" {
			at = v
		}
	}
	return time.ParseInLocation("2006-01-02 15:04", date+" "+at, time.Local)
}

func (s *Service) tickTodos(ctx context.Context, now time.Time) (int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id, t.title, t.kind, t.due_date, t.last_reminded_at, t.priority, COALESCE(l.name, '')
		FROM todos t LEFT JOIN todo_lists l ON l.id = t.list_id
		WHERE t.remind = 1 AND t.status != 'done' AND t.due_date IS NOT NULL AND t.due_date <= ?`, now.Local().Format("2006-01-02"))
	if err != nil {
		return 0, err
	}
	type due struct {
		id                     int64
		title, kind, date, lst string
		prio                   int
		due                    time.Time
	}
	var list []due
	for rows.Next() {
		var d due
		var last sql.NullString
		if rows.Scan(&d.id, &d.title, &d.kind, &d.date, &last, &d.prio, &d.lst) != nil {
			continue
		}
		t, err := s.todoDue(ctx, d.date)
		if err != nil {
			continue
		}
		d.due = t.UTC()
		if shouldSend(now, d.due, nil, parseTS(last)) {
			list = append(list, d)
		}
	}
	rows.Close()
	count := 0
	for _, d := range list {
		what := "Todo"
		if d.kind == "goal" {
			what = "Goal"
		}
		title := fmt.Sprintf("📋 %s due: %s", what, d.title)
		dueDay, _ := time.ParseInLocation("2006-01-02", d.date, time.Local)
		if today := now.Local().Format("2006-01-02"); d.date < today {
			title = fmt.Sprintf("📋 %s overdue: %s", what, d.title)
		}
		body := fmt.Sprintf("P%d · due %s", d.prio, dueDay.Format("Mon Jan 2"))
		if d.lst != "" {
			body += " · " + d.lst
		}
		if !s.Send(ctx, Message{Title: title, Body: body, Path: fmt.Sprintf("/todos?t=%d", d.id), LinkTitle: "Open todo"}) {
			return count, nil
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE todos SET last_reminded_at = ? WHERE id = ?`, now.Format(TS), d.id); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// ── reminder actions ──────────────────────────────────────────────────────

// Reminder is the schedule part of a reminder row.
type Reminder struct {
	ID     int64
	Kind   string
	Every  Every
	Anchor time.Time
	Due    time.Time
}

func (s *Service) load(ctx context.Context, id int64) (Reminder, error) {
	var r Reminder
	var anchor, due string
	err := s.DB.QueryRowContext(ctx, `SELECT id, kind, every_n, unit, anchor_at, due_at FROM reminders WHERE id = ?`, id).
		Scan(&r.ID, &r.Kind, &r.Every.N, &r.Every.Unit, &anchor, &due)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	a, err := time.Parse(TS, anchor)
	if err != nil {
		return r, err
	}
	d, err := time.Parse(TS, due)
	if err != nil {
		return r, err
	}
	r.Anchor, r.Due = a.Local(), d.Local()
	return r, nil
}

var ErrNotFound = errors.New("not found")

// nextDue works out the following occurrence after `now` (zero for once).
func nextDue(r Reminder, now time.Time) time.Time {
	switch r.Kind {
	case "calendar":
		after := now
		if r.Due.After(after) {
			after = r.Due // completing early moves past the current occurrence
		}
		return NextCalendar(r.Anchor, r.Every, after.In(r.Anchor.Location()))
	case "after_done":
		return NextAfterDone(r.Anchor, r.Every, now)
	}
	return time.Time{}
}

// Complete marks the current occurrence done and schedules the next.
func (s *Service) Complete(ctx context.Context, id int64, skip bool) error {
	r, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	now := s.Now()
	kind := "done"
	if skip {
		kind = "skipped"
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	nowS := now.UTC().Format(TS)
	if _, err := tx.ExecContext(ctx, `INSERT INTO reminder_events(reminder_id, ts, kind, due_at) VALUES (?, ?, ?, ?)`, id, nowS, kind, r.Due.UTC().Format(TS)); err != nil {
		return err
	}
	if r.Kind == "once" {
		_, err = tx.ExecContext(ctx, `UPDATE reminders SET enabled = 0, last_done_at = CASE WHEN ? THEN last_done_at ELSE ? END, snoozed_until = NULL WHERE id = ?`, skip, nowS, id)
	} else {
		next := nextDue(r, now)
		_, err = tx.ExecContext(ctx, `UPDATE reminders SET due_at = ?, last_sent_at = NULL, snoozed_until = NULL,
			last_done_at = CASE WHEN ? THEN last_done_at ELSE ? END WHERE id = ?`, next.UTC().Format(TS), skip, nowS, id)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.publish()
	return nil
}

// Snooze holds the reminder until `until`.
func (s *Service) Snooze(ctx context.Context, id int64, until time.Time) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE reminders SET snoozed_until = ? WHERE id = ?`, until.UTC().Format(TS), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO reminder_events(reminder_id, ts, kind) VALUES (?, ?, 'snoozed')`, id, s.Now().UTC().Format(TS))
	s.publish()
	return nil
}
