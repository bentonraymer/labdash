package monitor

import (
	"time"

	"github.com/robfig/cron/v3"
)

// Window is a maintenance window. One-off windows use Start/End; recurring
// windows use a cron expression for the start time plus a duration.
type Window struct {
	ID         int64
	Name       string
	Scope      string // global | service | host
	ScopeID    *int64
	Start, End time.Time
	Recurrence string
	Duration   time.Duration
	Enabled    bool
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ValidateCron reports whether expr is a valid 5-field cron expression.
func ValidateCron(expr string) error {
	_, err := cronParser.Parse(expr)
	return err
}

// ActiveAt reports whether the window covers t. Recurring windows are active
// if a start occurred within the last Duration — found by asking the
// schedule for the next start after (t - Duration).
func (w Window) ActiveAt(t time.Time) bool {
	if !w.Enabled {
		return false
	}
	if w.Recurrence == "" {
		return !t.Before(w.Start) && t.Before(w.End)
	}
	sched, err := cronParser.Parse(w.Recurrence)
	if err != nil || w.Duration <= 0 {
		return false
	}
	// The recurring series only runs between Start and End (End zero = forever).
	if !w.Start.IsZero() && t.Before(w.Start) {
		return false
	}
	if !w.End.IsZero() && !t.Before(w.End) {
		return false
	}
	next := sched.Next(t.Add(-w.Duration))
	return !next.After(t)
}

// Covers reports whether the window applies to a monitor for a service that
// runs on hostID.
func (w Window) Covers(serviceID, hostID *int64) bool {
	switch w.Scope {
	case "global":
		return true
	case "service":
		return w.ScopeID != nil && serviceID != nil && *w.ScopeID == *serviceID
	case "host":
		return w.ScopeID != nil && hostID != nil && *w.ScopeID == *hostID
	}
	return false
}
