// Package planning runs reminders (one-off and recurring) and todo due-date
// reminders, delivering them through Pushover and re-sending daily until
// they're marked done.
package planning

import (
	"fmt"
	"time"
)

// TS is the stored timestamp format (UTC).
const TS = "2006-01-02T15:04:05.000Z"

// Every is a repeat interval like "every 3 months".
type Every struct {
	N    int
	Unit string // day | week | month | quarter | year
}

// Validate checks the unit and count.
func (e Every) Validate() error {
	switch e.Unit {
	case "day", "week", "month", "quarter", "year":
	default:
		return fmt.Errorf("unknown unit %q", e.Unit)
	}
	if e.N < 1 || e.N > 1000 {
		return fmt.Errorf("repeat every 1–1000 %ss", e.Unit)
	}
	return nil
}

// months is the interval in months (0 for day/week units).
func (e Every) months() int {
	switch e.Unit {
	case "month":
		return e.N
	case "quarter":
		return 3 * e.N
	case "year":
		return 12 * e.N
	}
	return 0
}

// Add returns t moved forward k intervals, keeping the time of day and
// clamping to the end of shorter months (Jan 31 + 1 month = Feb 28).
func (e Every) Add(t time.Time, k int) time.Time {
	switch e.Unit {
	case "day":
		return t.AddDate(0, 0, e.N*k)
	case "week":
		return t.AddDate(0, 0, 7*e.N*k)
	}
	return addMonthsClamped(t, e.months()*k)
}

func addMonthsClamped(t time.Time, m int) time.Time {
	y, mo, d := t.Date()
	first := time.Date(y, mo, 1, t.Hour(), t.Minute(), t.Second(), 0, t.Location()).AddDate(0, m, 0)
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, t.Hour(), t.Minute(), t.Second(), 0, t.Location())
}

// NextCalendar returns the first occurrence of anchor + k*every that is
// strictly after `after` (k >= 0). Each occurrence is computed from the
// anchor so month-end clamping never drifts.
func NextCalendar(anchor time.Time, e Every, after time.Time) time.Time {
	if anchor.After(after) {
		return anchor
	}
	// Jump close first, then step.
	k := 0
	switch e.Unit {
	case "day", "week":
		step := e.Add(anchor, 1).Sub(anchor)
		if step > 0 {
			k = int(after.Sub(anchor) / step)
		}
	default:
		if m := e.months(); m > 0 {
			k = ((after.Year()-anchor.Year())*12 + int(after.Month()) - int(anchor.Month())) / m
		}
	}
	if k < 0 {
		k = 0
	}
	for k > 0 && e.Add(anchor, k).After(after) {
		k--
	}
	for !e.Add(anchor, k).After(after) {
		k++
	}
	return e.Add(anchor, k)
}

// NextAfterDone is the next due time for "N units after completion": the
// completion date plus the interval, at the anchor's time of day.
func NextAfterDone(anchor time.Time, e Every, done time.Time) time.Time {
	done = done.In(anchor.Location())
	base := time.Date(done.Year(), done.Month(), done.Day(), anchor.Hour(), anchor.Minute(), 0, 0, anchor.Location())
	return e.Add(base, 1)
}

// Describe renders a schedule for people ("Every 3 months", "Quarterly").
func Describe(kind string, e Every) string {
	if kind == "once" {
		return "Once"
	}
	names := map[string][2]string{
		"day": {"Daily", "days"}, "week": {"Weekly", "weeks"}, "month": {"Monthly", "months"},
		"quarter": {"Quarterly", "quarters"}, "year": {"Yearly", "years"},
	}
	n := names[e.Unit]
	s := n[0]
	if e.N > 1 {
		s = fmt.Sprintf("Every %d %s", e.N, n[1])
	}
	if kind == "after_done" {
		if e.N == 1 {
			s = fmt.Sprintf("1 %s after done", e.Unit)
		} else {
			s = fmt.Sprintf("%d %s after done", e.N, n[1])
		}
	}
	return s
}
