package monitor

import (
	"testing"
	"time"
)

func TestWindowActive(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	one := Window{Enabled: true, Start: at("2026-11-07T02:00:00Z"), End: at("2026-11-07T04:00:00Z")}
	if !one.ActiveAt(at("2026-11-07T03:00:00Z")) || one.ActiveAt(at("2026-11-07T04:00:00Z")) || one.ActiveAt(at("2026-11-07T01:59:59Z")) {
		t.Error("one-off window boundaries wrong")
	}
	// Sundays 03:00 for 90 minutes (cron uses the process TZ; tests run in UTC).
	weekly := Window{Enabled: true, Recurrence: "0 3 * * 0", Duration: 90 * time.Minute}
	if !weekly.ActiveAt(at("2026-11-08T03:00:00Z")) || !weekly.ActiveAt(at("2026-11-08T04:29:00Z")) {
		t.Error("recurring window should be active Sunday 03:00–04:30")
	}
	if weekly.ActiveAt(at("2026-11-08T04:31:00Z")) || weekly.ActiveAt(at("2026-11-09T03:10:00Z")) {
		t.Error("recurring window should be inactive outside its slot")
	}
	disabled := one
	disabled.Enabled = false
	if disabled.ActiveAt(at("2026-11-07T03:00:00Z")) {
		t.Error("disabled window must not be active")
	}
}

func TestWindowCovers(t *testing.T) {
	id := func(v int64) *int64 { return &v }
	if !(Window{Scope: "global"}).Covers(nil, nil) {
		t.Error("global covers all")
	}
	if !(Window{Scope: "service", ScopeID: id(3)}).Covers(id(3), nil) || (Window{Scope: "service", ScopeID: id(3)}).Covers(id(4), nil) {
		t.Error("service scope")
	}
	if !(Window{Scope: "host", ScopeID: id(1)}).Covers(id(9), id(1)) || (Window{Scope: "host", ScopeID: id(1)}).Covers(id(9), nil) {
		t.Error("host scope")
	}
}
