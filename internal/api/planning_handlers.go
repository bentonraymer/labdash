package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/bentonraymer/labdash/internal/planning"
)

// ── reminders ─────────────────────────────────────────────────────────────

type ReminderEvent struct {
	TS    string `json:"ts"`
	Kind  string `json:"kind"`
	DueAt string `json:"dueAt"`
}

type Reminder struct {
	ID           int64           `json:"id"`
	Title        string          `json:"title"`
	Notes        string          `json:"notes"`
	Kind         string          `json:"kind"` // once | calendar | after_done
	EveryN       int             `json:"everyN"`
	Unit         string          `json:"unit"`
	AnchorAt     string          `json:"anchorAt"`
	DueAt        string          `json:"dueAt"`
	Enabled      bool            `json:"enabled"`
	SnoozedUntil *string         `json:"snoozedUntil"`
	LastSentAt   *string         `json:"lastSentAt"`
	LastDoneAt   *string         `json:"lastDoneAt"`
	Schedule     string          `json:"schedule"` // human description
	State        string          `json:"state"`    // due | snoozed | upcoming | done | paused
	Events       []ReminderEvent `json:"events"`
}

func nullStr(n sql.NullString) *string {
	if n.Valid {
		return &n.String
	}
	return nil
}

func (s *Server) listReminders(ctx context.Context) ([]*Reminder, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, title, notes, kind, every_n, unit, anchor_at, due_at, enabled, snoozed_until, last_sent_at, last_done_at
		FROM reminders ORDER BY enabled DESC, due_at`)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(planning.TS)
	out := []*Reminder{}
	byID := map[int64]*Reminder{}
	for rows.Next() {
		var r Reminder
		var snz, sent, done sql.NullString
		if err := rows.Scan(&r.ID, &r.Title, &r.Notes, &r.Kind, &r.EveryN, &r.Unit, &r.AnchorAt, &r.DueAt, &r.Enabled, &snz, &sent, &done); err != nil {
			rows.Close()
			return nil, err
		}
		r.SnoozedUntil, r.LastSentAt, r.LastDoneAt = nullStr(snz), nullStr(sent), nullStr(done)
		r.Schedule = planning.Describe(r.Kind, planning.Every{N: r.EveryN, Unit: r.Unit})
		switch {
		case !r.Enabled && r.Kind == "once" && r.LastDoneAt != nil:
			r.State = "done"
		case !r.Enabled:
			r.State = "paused"
		case r.SnoozedUntil != nil && *r.SnoozedUntil > now:
			r.State = "snoozed"
		case r.DueAt <= now:
			r.State = "due"
		default:
			r.State = "upcoming"
		}
		r.Events = []ReminderEvent{}
		out = append(out, &r)
		byID[r.ID] = &r
	}
	rows.Close()
	// Last few done/skipped events per reminder (history).
	rows, err = s.DB.QueryContext(ctx, `SELECT reminder_id, ts, kind, due_at FROM reminder_events WHERE kind IN ('done','skipped') ORDER BY ts DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var e ReminderEvent
		if rows.Scan(&id, &e.TS, &e.Kind, &e.DueAt) == nil {
			if r := byID[id]; r != nil && len(r.Events) < 8 {
				r.Events = append(r.Events, e)
			}
		}
	}
	return out, rows.Err()
}

func (s *Server) handleListReminders(w http.ResponseWriter, r *http.Request) {
	out, err := s.listReminders(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type reminderInput struct {
	Title    string `json:"title"`
	Notes    string `json:"notes"`
	Kind     string `json:"kind"`
	EveryN   int    `json:"everyN"`
	Unit     string `json:"unit"`
	AnchorAt string `json:"anchorAt"` // RFC 3339 / ISO from the browser
	Enabled  *bool  `json:"enabled"`
}

func (in *reminderInput) validate() (anchor time.Time, err error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 200 {
		return anchor, errors.New("enter a title (up to 200 characters)")
	}
	if len(in.Notes) > 10000 {
		return anchor, errors.New("notes are too long")
	}
	if in.Kind == "" {
		in.Kind = "once"
	}
	if oneOf("once", "calendar", "after_done")(in.Kind) != nil {
		return anchor, errors.New("unknown schedule")
	}
	if in.EveryN == 0 {
		in.EveryN = 1
	}
	if in.Unit == "" {
		in.Unit = "month"
	}
	if err := (planning.Every{N: in.EveryN, Unit: in.Unit}).Validate(); err != nil {
		return anchor, err
	}
	anchor, err = time.Parse(time.RFC3339, in.AnchorAt)
	if err != nil {
		return anchor, errors.New("pick a date and time")
	}
	return anchor.UTC().Truncate(time.Minute), nil
}

// dueFor is the occurrence to schedule when a reminder is saved: the anchor
// itself if it's in the future, else the next calendar slot (recurring), or
// the anchor (past one-offs are due right away).
func dueFor(in reminderInput, anchor time.Time) time.Time {
	now := time.Now()
	if in.Kind == "calendar" && !anchor.After(now) {
		return planning.NextCalendar(anchor.Local(), planning.Every{N: in.EveryN, Unit: in.Unit}, now).UTC()
	}
	return anchor
}

func (s *Server) handleCreateReminder(w http.ResponseWriter, r *http.Request) {
	var in reminderInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	anchor, err := in.validate()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.DB.ExecContext(r.Context(), `INSERT INTO reminders(title, notes, kind, every_n, unit, anchor_at, due_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		in.Title, in.Notes, in.Kind, in.EveryN, in.Unit, anchor.Format(planning.TS), dueFor(in, anchor).Format(planning.TS))
	if err != nil {
		serverErr(w, err)
		return
	}
	id, _ := res.LastInsertId()
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) handleUpdateReminder(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in reminderInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	anchor, err := in.validate()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Re-plan from the (possibly new) schedule; re-enabling a finished
	// one-off brings it back.
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	res, err := s.DB.ExecContext(r.Context(), `UPDATE reminders SET title = ?, notes = ?, kind = ?, every_n = ?, unit = ?, anchor_at = ?,
		due_at = ?, enabled = ?, snoozed_until = NULL, last_sent_at = CASE WHEN due_at = ? THEN last_sent_at ELSE NULL END WHERE id = ?`,
		in.Title, in.Notes, in.Kind, in.EveryN, in.Unit, anchor.Format(planning.TS), dueFor(in, anchor).Format(planning.TS), enabled,
		dueFor(in, anchor).Format(planning.TS), id)
	if err != nil {
		serverErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteReminder(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM reminders WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/reminders/{id}/{action}: done | skip | snooze {until} | test
func (s *Server) handleReminderAction(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Planning == nil {
		writeErr(w, http.StatusServiceUnavailable, "reminders are unavailable")
		return
	}
	ctx := r.Context()
	switch action := chi.URLParam(r, "action"); action {
	case "done", "skip":
		err = s.Planning.Complete(ctx, id, action == "skip")
	case "snooze":
		var in struct {
			Until string `json:"until"`
		}
		_ = readJSON(r, &in)
		until, perr := time.Parse(time.RFC3339, in.Until)
		if perr != nil || !until.After(time.Now()) {
			writeErr(w, http.StatusBadRequest, "pick a time in the future")
			return
		}
		err = s.Planning.Snooze(ctx, id, until)
	case "test":
		var title, notes string
		if qerr := s.DB.QueryRowContext(ctx, `SELECT title, notes FROM reminders WHERE id = ?`, id).Scan(&title, &notes); qerr != nil {
			err = planning.ErrNotFound
			break
		}
		if s.Planning.Send == nil || !s.Planning.Send(ctx, planning.Message{Title: "⏰ " + title + " (test)", Body: notes, Path: fmt.Sprintf("/reminders?r=%d", id), LinkTitle: "Open reminder"}) {
			writeErr(w, http.StatusConflict, "alerts are paused (Settings → General), so nothing was sent")
			return
		}
	default:
		writeErr(w, http.StatusNotFound, "unknown action")
		return
	}
	if errors.Is(err, planning.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── todos ─────────────────────────────────────────────────────────────────

type ChecklistItem struct {
	ID       int64  `json:"id"`
	Text     string `json:"text"`
	Done     bool   `json:"done"`
	Position int    `json:"position"`
}

type Todo struct {
	ID          int64           `json:"id"`
	ListID      *int64          `json:"listId"`
	ParentID    *int64          `json:"parentId"`
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Notes       string          `json:"notes"`
	Priority    int             `json:"priority"`
	Status      string          `json:"status"`
	DueDate     *string         `json:"dueDate"`
	Remind      bool            `json:"remind"`
	Position    float64         `json:"position"`
	ServiceID   *int64          `json:"serviceId"`
	ServiceName string          `json:"serviceName"`
	ServiceIcon string          `json:"serviceIcon"`
	HostID      *int64          `json:"hostId"`
	HostName    string          `json:"hostName"`
	CreatedAt   string          `json:"createdAt"`
	UpdatedAt   string          `json:"updatedAt"`
	CompletedAt *string         `json:"completedAt"`
	Checklist   []ChecklistItem `json:"checklist"`
	// Goals: progress over their tasks.
	TasksTotal int `json:"tasksTotal"`
	TasksDone  int `json:"tasksDone"`
}

type TodoList struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Open     int    `json:"open"`
}

// GET /api/todos: everything (homelab-sized), with checklists and goal progress.
func (s *Server) handleListTodos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id, t.list_id, t.parent_id, t.kind, t.title, t.notes, t.priority, t.status, t.due_date, t.remind,
		t.position, t.service_id, COALESCE(sv.name, ''), COALESCE(sv.icon, ''), t.host_id, COALESCE(h.name, ''), t.created_at, t.updated_at, t.completed_at
		FROM todos t LEFT JOIN services sv ON sv.id = t.service_id LEFT JOIN hosts h ON h.id = t.host_id
		ORDER BY t.priority, t.position, t.id`)
	if err != nil {
		serverErr(w, err)
		return
	}
	todos := []*Todo{}
	byID := map[int64]*Todo{}
	for rows.Next() {
		var t Todo
		var list, parent, svc, host sql.NullInt64
		var due, done sql.NullString
		if err := rows.Scan(&t.ID, &list, &parent, &t.Kind, &t.Title, &t.Notes, &t.Priority, &t.Status, &due, &t.Remind, &t.Position,
			&svc, &t.ServiceName, &t.ServiceIcon, &host, &t.HostName, &t.CreatedAt, &t.UpdatedAt, &done); err != nil {
			rows.Close()
			serverErr(w, err)
			return
		}
		if list.Valid {
			t.ListID = &list.Int64
		}
		if parent.Valid {
			t.ParentID = &parent.Int64
		}
		if svc.Valid {
			t.ServiceID = &svc.Int64
		}
		if host.Valid {
			t.HostID = &host.Int64
		}
		t.DueDate, t.CompletedAt = nullStr(due), nullStr(done)
		t.Checklist = []ChecklistItem{}
		todos = append(todos, &t)
		byID[t.ID] = &t
	}
	rows.Close()
	for _, t := range todos {
		if t.ParentID != nil {
			if g := byID[*t.ParentID]; g != nil {
				g.TasksTotal++
				if t.Status == "done" {
					g.TasksDone++
				}
			}
		}
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT id, todo_id, text, done, position FROM todo_checklist ORDER BY position, id`)
	if err != nil {
		serverErr(w, err)
		return
	}
	for rows.Next() {
		var c ChecklistItem
		var tid int64
		if rows.Scan(&c.ID, &tid, &c.Text, &c.Done, &c.Position) == nil {
			if t := byID[tid]; t != nil {
				t.Checklist = append(t.Checklist, c)
			}
		}
	}
	rows.Close()

	lists := []TodoList{}
	rows, err = s.DB.QueryContext(ctx, `SELECT l.id, l.name, l.position, (SELECT COUNT(*) FROM todos t WHERE t.list_id = l.id AND t.status != 'done')
		FROM todo_lists l ORDER BY l.position, l.id`)
	if err != nil {
		serverErr(w, err)
		return
	}
	for rows.Next() {
		var l TodoList
		if rows.Scan(&l.ID, &l.Name, &l.Position, &l.Open) == nil {
			lists = append(lists, l)
		}
	}
	rows.Close()
	writeJSON(w, http.StatusOK, map[string]any{"todos": todos, "lists": lists})
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// todoPatch: every field optional; Clear* removes a link/date.
type todoPatch struct {
	ListID      *int64   `json:"listId"`
	ClearList   bool     `json:"clearList"`
	ParentID    *int64   `json:"parentId"`
	ClearParent bool     `json:"clearParent"`
	Kind        *string  `json:"kind"`
	Title       *string  `json:"title"`
	Notes       *string  `json:"notes"`
	Priority    *int     `json:"priority"`
	Status      *string  `json:"status"`
	DueDate     *string  `json:"dueDate"`
	ClearDue    bool     `json:"clearDue"`
	Remind      *bool    `json:"remind"`
	Position    *float64 `json:"position"`
	ServiceID   *int64   `json:"serviceId"`
	ClearSvc    bool     `json:"clearService"`
	HostID      *int64   `json:"hostId"`
	ClearHost   bool     `json:"clearHost"`
}

func (p *todoPatch) validate() error {
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		if t == "" || len(t) > 300 {
			return errors.New("enter a title (up to 300 characters)")
		}
		p.Title = &t
	}
	if p.Notes != nil && len(*p.Notes) > 50000 {
		return errors.New("notes are too long")
	}
	if p.Kind != nil && oneOf("task", "goal")(*p.Kind) != nil {
		return errors.New("kind must be task or goal")
	}
	if p.Priority != nil && (*p.Priority < 1 || *p.Priority > 4) {
		return errors.New("priority must be 1–4")
	}
	if p.Status != nil && oneOf("open", "doing", "done")(*p.Status) != nil {
		return errors.New("status must be open, doing or done")
	}
	if p.DueDate != nil && *p.DueDate != "" && !dateRe.MatchString(*p.DueDate) {
		return errors.New("due date must be YYYY-MM-DD")
	}
	if p.DueDate != nil && *p.DueDate == "" {
		p.DueDate, p.ClearDue = nil, true
	}
	return nil
}

// checkParent makes sure a task's parent is a goal and not itself.
func (s *Server) checkTodoParent(ctx context.Context, id, parent int64) error {
	if parent == id {
		return errors.New("a goal can't contain itself")
	}
	var kind string
	if err := s.DB.QueryRowContext(ctx, `SELECT kind FROM todos WHERE id = ?`, parent).Scan(&kind); err != nil {
		return errors.New("that goal doesn't exist")
	}
	if kind != "goal" {
		return errors.New("tasks can only be added to goals")
	}
	return nil
}

func (s *Server) handleCreateTodo(w http.ResponseWriter, r *http.Request) {
	var p todoPatch
	if err := readJSON(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.Title == nil {
		writeErr(w, http.StatusBadRequest, "enter a title")
		return
	}
	if err := p.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	if p.ParentID != nil {
		if err := s.checkTodoParent(ctx, 0, *p.ParentID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	kind, prio, notes := "task", 3, ""
	if p.Kind != nil {
		kind = *p.Kind
	}
	if p.Priority != nil {
		prio = *p.Priority
	}
	if p.Notes != nil {
		notes = *p.Notes
	}
	remind := p.Remind != nil && *p.Remind
	// New items go to the end of their priority.
	var pos float64
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM todos WHERE priority = ?`, prio).Scan(&pos)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO todos(list_id, parent_id, kind, title, notes, priority, due_date, remind, position, service_id, host_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.ListID, p.ParentID, kind, *p.Title, notes, prio, p.DueDate, remind, pos, p.ServiceID, p.HostID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "couldn't save: check the list, goal, service and host still exist")
		return
	}
	id, _ := res.LastInsertId()
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) handleUpdateTodo(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var p todoPatch
	if err := readJSON(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := p.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	if p.ParentID != nil {
		if err := s.checkTodoParent(ctx, id, *p.ParentID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	sets := []string{"updated_at = ?"}
	args := []any{time.Now().UTC().Format(planning.TS)}
	add := func(col string, v any) {
		sets = append(sets, col+" = ?")
		args = append(args, v)
	}
	if p.ListID != nil {
		add("list_id", *p.ListID)
	} else if p.ClearList {
		add("list_id", nil)
	}
	if p.ParentID != nil {
		add("parent_id", *p.ParentID)
	} else if p.ClearParent {
		add("parent_id", nil)
	}
	if p.Kind != nil {
		add("kind", *p.Kind)
		if *p.Kind == "task" {
			// Turning a goal back into a task releases its tasks.
			_, _ = s.DB.ExecContext(ctx, `UPDATE todos SET parent_id = NULL WHERE parent_id = ?`, id)
		}
	}
	if p.Title != nil {
		add("title", *p.Title)
	}
	if p.Notes != nil {
		add("notes", *p.Notes)
	}
	if p.Priority != nil {
		add("priority", *p.Priority)
	}
	if p.Status != nil {
		add("status", *p.Status)
		if *p.Status == "done" {
			add("completed_at", time.Now().UTC().Format(planning.TS))
		} else {
			add("completed_at", nil)
		}
	}
	if p.DueDate != nil {
		add("due_date", *p.DueDate)
		add("last_reminded_at", nil) // a new date gets a fresh reminder
	} else if p.ClearDue {
		add("due_date", nil)
	}
	if p.Remind != nil {
		add("remind", *p.Remind)
	}
	if p.Position != nil {
		add("position", *p.Position)
	}
	if p.ServiceID != nil {
		add("service_id", *p.ServiceID)
	} else if p.ClearSvc {
		add("service_id", nil)
	}
	if p.HostID != nil {
		add("host_id", *p.HostID)
	} else if p.ClearHost {
		add("host_id", nil)
	}
	args = append(args, id)
	res, err := s.DB.ExecContext(ctx, `UPDATE todos SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "couldn't save: check the list, goal, service and host still exist")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteTodo(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM todos WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/todos/reorder {ids: [...]}: positions follow the given order.
func (s *Server) handleReorderTodos(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &in); err != nil || len(in.IDs) > 5000 {
		writeErr(w, http.StatusBadRequest, "bad order")
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		serverErr(w, err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	for i, id := range in.IDs {
		if _, err := tx.ExecContext(r.Context(), `UPDATE todos SET position = ? WHERE id = ?`, float64(i+1), id); err != nil {
			serverErr(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── checklist ─────────────────────────────────────────────────────────────

func (s *Server) handleAddChecklist(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.Text) == "" || len(in.Text) > 500 {
		writeErr(w, http.StatusBadRequest, "enter the item text")
		return
	}
	res, err := s.DB.ExecContext(r.Context(), `INSERT INTO todo_checklist(todo_id, text, position)
		VALUES (?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM todo_checklist WHERE todo_id = ?))`, id, strings.TrimSpace(in.Text), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "todo not found")
		return
	}
	cid, _ := res.LastInsertId()
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusCreated, map[string]int64{"id": cid})
}

func (s *Server) handleUpdateChecklist(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var in struct {
		Text *string `json:"text"`
		Done *bool   `json:"done"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Text != nil && (strings.TrimSpace(*in.Text) == "" || len(*in.Text) > 500) {
		writeErr(w, http.StatusBadRequest, "enter the item text")
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE todo_checklist SET text = COALESCE(?, text), done = COALESCE(?, done) WHERE id = ?`, in.Text, in.Done, id); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteChecklist(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM todo_checklist WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── lists ─────────────────────────────────────────────────────────────────

func (s *Server) handleSaveTodoList(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" || len(in.Name) > 80 {
		writeErr(w, http.StatusBadRequest, "enter a list name")
		return
	}
	name := strings.TrimSpace(in.Name)
	if r.Method == http.MethodPost {
		res, err := s.DB.ExecContext(r.Context(), `INSERT INTO todo_lists(name, position) VALUES (?, (SELECT COALESCE(MAX(position), 0) + 1 FROM todo_lists))`, name)
		if err != nil {
			serverErr(w, err)
			return
		}
		id, _ := res.LastInsertId()
		s.Events.Publish("planning", nil)
		writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE todo_lists SET name = ? WHERE id = ?`, name, id); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// DELETE /api/todo-lists/{id}: its todos move to the Inbox (not deleted).
func (s *Server) handleDeleteTodoList(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM todo_lists WHERE id = ?`, id); err != nil {
		serverErr(w, err)
		return
	}
	s.Events.Publish("planning", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── summary (nav badges, service/host counts) ─────────────────────────────

func (s *Server) handlePlanningSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()
	nowS := now.UTC().Format(planning.TS)
	today := now.Format("2006-01-02")
	out := map[string]any{}
	var due, overdue, dueToday int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reminders WHERE enabled = 1 AND due_at <= ? AND (snoozed_until IS NULL OR snoozed_until <= ?)`, nowS, nowS).Scan(&due)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM todos WHERE status != 'done' AND due_date < ?`, today).Scan(&overdue)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM todos WHERE status != 'done' AND due_date = ?`, today).Scan(&dueToday)
	out["remindersDue"], out["todosOverdue"], out["todosDueToday"] = due, overdue, dueToday
	bySvc, byHost := map[string]int{}, map[string]int{}
	rows, err := s.DB.QueryContext(ctx, `SELECT service_id, host_id FROM todos WHERE status != 'done' AND (service_id IS NOT NULL OR host_id IS NOT NULL)`)
	if err == nil {
		for rows.Next() {
			var svc, host sql.NullInt64
			if rows.Scan(&svc, &host) == nil {
				if svc.Valid {
					bySvc[fmt.Sprint(svc.Int64)]++
				}
				if host.Valid {
					byHost[fmt.Sprint(host.Int64)]++
				}
			}
		}
		rows.Close()
	}
	out["openByService"], out["openByHost"] = bySvc, byHost
	writeJSON(w, http.StatusOK, out)
}
