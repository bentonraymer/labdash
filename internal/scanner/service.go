package scanner

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/bentonraymer/labdash/internal/events"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ValidateSchedule checks a 5-field cron expression ("" = manual only).
func ValidateSchedule(expr string) error {
	if expr == "" {
		return nil
	}
	_, err := cronParser.Parse(expr)
	return err
}

// Status is the live state of the scanner.
type Status struct {
	Running  bool    `json:"running"`
	TargetID int64   `json:"targetId"`
	CIDR     string  `json:"cidr"`
	Phase    string  `json:"phase"`
	Percent  float64 `json:"percent"`
	Started  string  `json:"started"`
	Queued   []int64 `json:"queued"`
}

// NewDeviceNotifier is called with devices first seen in a scan.
type NewDeviceNotifier func(ctx context.Context, devices []NewDevice)

// Service schedules and runs scans one at a time.
type Service struct {
	DB      *sql.DB
	Hub     *events.Hub
	Prober  Prober
	OnNew   NewDeviceNotifier
	RunNmap func(ctx context.Context, target string, prof Profile, onProgress func(Progress)) ([]Host, error)
	started time.Time
	mu      sync.Mutex
	status  Status
	queue   []int64
	wake    chan struct{}
}

func NewService(db *sql.DB, hub *events.Hub, prober Prober, onNew NewDeviceNotifier) *Service {
	return &Service{DB: db, Hub: hub, Prober: prober, OnNew: onNew, RunNmap: RunNmap, wake: make(chan struct{}, 1), started: time.Now()}
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Queued = append([]int64{}, s.queue...)
	return st
}

// Enqueue requests a scan of a target (no-op if already queued/running).
func (s *Service) Enqueue(targetID int64) {
	s.mu.Lock()
	if s.status.Running && s.status.TargetID == targetID {
		s.mu.Unlock()
		return
	}
	for _, q := range s.queue {
		if q == targetID {
			s.mu.Unlock()
			return
		}
	}
	s.queue = append(s.queue, targetID)
	s.mu.Unlock()
	s.publish()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) publish() { s.Hub.Publish("scan", s.Status()) }

// Run processes the queue and enqueues targets whose schedule is due.
func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		s.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-tick.C:
			s.enqueueDue(ctx)
		}
	}
}

func (s *Service) enqueueDue(ctx context.Context) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, schedule, last_run_at FROM scan_targets WHERE enabled = 1 AND schedule != ''`)
	if err != nil {
		return
	}
	var due []int64
	now := time.Now()
	for rows.Next() {
		var id int64
		var sched string
		var last sql.NullString
		if rows.Scan(&id, &sched, &last) != nil {
			continue
		}
		sc, err := cronParser.Parse(sched)
		if err != nil {
			continue
		}
		from := s.started // never run: first slot after LabDash started
		if last.Valid {
			if t, err := time.Parse(tsFormat, last.String); err == nil {
				from = t.Local()
			}
		}
		if !sc.Next(from).After(now) {
			due = append(due, id)
		}
	}
	rows.Close()
	for _, id := range due {
		s.Enqueue(id)
	}
}

func (s *Service) drain(ctx context.Context) {
	for ctx.Err() == nil {
		s.mu.Lock()
		if len(s.queue) == 0 {
			s.mu.Unlock()
			return
		}
		id := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		if err := s.scanTarget(ctx, id); err != nil && ctx.Err() == nil {
			slog.Warn("scan failed", "target", id, "err", err)
		}
	}
}

func (s *Service) scanTarget(ctx context.Context, id int64) error {
	var cidr, profile, custom string
	var lastRun sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT cidr, port_profile, custom_ports, last_run_at FROM scan_targets WHERE id = ?`, id).Scan(&cidr, &profile, &custom, &lastRun)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	startedAt := time.Now().UTC().Format(tsFormat)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO scans(target_id, cidr, started_at) VALUES (?, ?, ?)`, id, cidr, startedAt)
	if err != nil {
		return err
	}
	scanID, _ := res.LastInsertId()

	s.mu.Lock()
	s.status = Status{Running: true, TargetID: id, CIDR: cidr, Phase: "Starting", Started: startedAt}
	s.mu.Unlock()
	s.publish()
	defer func() {
		s.mu.Lock()
		s.status = Status{}
		s.mu.Unlock()
		s.publish()
	}()

	var lastPub time.Time
	hosts, err := s.RunNmap(ctx, cidr, Profile{Name: profile, Custom: custom}, func(p Progress) {
		s.mu.Lock()
		s.status.Phase = p.Phase
		if p.Percent >= 0 {
			s.status.Percent = p.Percent
		}
		s.mu.Unlock()
		if time.Since(lastPub) > time.Second {
			lastPub = time.Now()
			s.publish()
		}
	})
	if err == nil {
		s.mu.Lock()
		s.status.Phase, s.status.Percent = "Fetching web titles & favicons", 100
		s.mu.Unlock()
		s.publish()
		EnrichWeb(ctx, hosts, s.Prober)
	}
	finished := time.Now().UTC().Format(tsFormat)
	if err != nil {
		_, _ = s.DB.ExecContext(context.Background(), `UPDATE scans SET status = 'failed', finished_at = ?, error = ? WHERE id = ?`, finished, err.Error(), scanID)
		return err
	}
	sum, err := Persist(ctx, s.DB, cidr, hosts, !lastRun.Valid)
	if err != nil {
		_, _ = s.DB.ExecContext(context.Background(), `UPDATE scans SET status = 'failed', finished_at = ?, error = ? WHERE id = ?`, finished, err.Error(), scanID)
		return err
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE scans SET status = 'done', finished_at = ?, hosts_up = ? WHERE id = ?`, finished, sum.HostsUp, scanID)
	_, _ = s.DB.ExecContext(ctx, `UPDATE scan_targets SET last_run_at = ? WHERE id = ?`, finished, id)
	slog.Info("scan done", "cidr", cidr, "up", sum.HostsUp, "new", len(sum.New), "down", sum.WentDown)
	s.Hub.Publish("devices", map[string]any{"targetId": id, "new": len(sum.New)})
	if len(sum.New) > 0 && s.OnNew != nil {
		go s.OnNew(context.Background(), sum.New)
	}
	return nil
}
