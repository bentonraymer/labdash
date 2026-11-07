// Package monitor runs uptime checks, tracks state transitions, records
// heartbeats/incidents and triggers notifications.
package monitor

// Heartbeat "up" column values.
const (
	BeatDown        = 0
	BeatUp          = 1
	BeatMaintenance = 2
	BeatPending     = 3 // failed check still within the retry budget
)

// State is the persisted status of a monitor.
type State struct {
	Status   string // pending | up | down | maintenance
	Failures int    // consecutive failed checks
}

// Outcome is the result of applying one check result to a State.
type Outcome struct {
	State
	Beat    int
	Changed bool   // Status differs from the previous status
	Notify  string // "down", "up" or "" — what (if anything) to alert about
}

// Transition applies a check result. A monitor goes down only after
// `retries` additional consecutive failures (so retries=2 → 3 failures).
// Recovery from down alerts "up". Maintenance suppresses state changes.
func Transition(cur State, up bool, retries int, inMaintenance bool) Outcome {
	if retries < 0 {
		retries = 0
	}
	var o Outcome
	switch {
	case inMaintenance:
		o.State = State{Status: "maintenance", Failures: 0}
		o.Beat = BeatMaintenance
	case up:
		o.State = State{Status: "up", Failures: 0}
		o.Beat = BeatUp
		if cur.Status == "down" {
			o.Notify = "up"
		}
	default:
		f := cur.Failures + 1
		if f > retries {
			o.State = State{Status: "down", Failures: f}
			o.Beat = BeatDown
			if cur.Status != "down" {
				o.Notify = "down"
			}
		} else {
			// Still within retry budget: hold "up" if we were up, else pending.
			st := cur.Status
			if st != "up" {
				st = "pending"
			}
			o.State = State{Status: st, Failures: f}
			o.Beat = BeatPending
		}
	}
	o.Changed = o.Status != cur.Status
	return o
}
