package monitor

import "testing"

func TestTransition(t *testing.T) {
	type step struct {
		up, maint  bool
		wantStatus string
		wantNotify string
		wantBeat   int
	}
	run := func(name string, start State, retries int, steps []step) {
		t.Helper()
		s := start
		for i, st := range steps {
			o := Transition(s, st.up, retries, st.maint)
			if o.Status != st.wantStatus || o.Notify != st.wantNotify || o.Beat != st.wantBeat {
				t.Errorf("%s step %d: got status=%s notify=%q beat=%d, want %s %q %d",
					name, i, o.Status, o.Notify, o.Beat, st.wantStatus, st.wantNotify, st.wantBeat)
			}
			s = o.State
		}
	}

	run("first success from pending is silent", State{Status: "pending"}, 2, []step{
		{up: true, wantStatus: "up", wantBeat: BeatUp},
	})
	run("retries absorb blips", State{Status: "up"}, 2, []step{
		{up: false, wantStatus: "up", wantBeat: BeatPending},
		{up: false, wantStatus: "up", wantBeat: BeatPending},
		{up: true, wantStatus: "up", wantBeat: BeatUp},
	})
	run("down after retries, alert once, recover", State{Status: "up"}, 2, []step{
		{up: false, wantStatus: "up", wantBeat: BeatPending},
		{up: false, wantStatus: "up", wantBeat: BeatPending},
		{up: false, wantStatus: "down", wantNotify: "down", wantBeat: BeatDown},
		{up: false, wantStatus: "down", wantBeat: BeatDown},
		{up: true, wantStatus: "up", wantNotify: "up", wantBeat: BeatUp},
	})
	run("zero retries goes down immediately", State{Status: "up"}, 0, []step{
		{up: false, wantStatus: "down", wantNotify: "down", wantBeat: BeatDown},
	})
	run("never-up monitor goes pending then down", State{Status: "pending"}, 1, []step{
		{up: false, wantStatus: "pending", wantBeat: BeatPending},
		{up: false, wantStatus: "down", wantNotify: "down", wantBeat: BeatDown},
	})
	run("maintenance suppresses and resumes cleanly", State{Status: "down", Failures: 5}, 1, []step{
		{maint: true, wantStatus: "maintenance", wantBeat: BeatMaintenance},
		{up: false, wantStatus: "pending", wantBeat: BeatPending},
		{up: false, wantStatus: "down", wantNotify: "down", wantBeat: BeatDown},
	})
}
