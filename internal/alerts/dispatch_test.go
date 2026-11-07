package alerts

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bentonraymer/labdash/internal/monitor"
	"github.com/bentonraymer/labdash/internal/pushover"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

type sink struct {
	mu   sync.Mutex
	msgs []pushover.Message
}

func (s *sink) send(_ context.Context, m pushover.Message) error {
	s.mu.Lock()
	s.msgs = append(s.msgs, m)
	s.mu.Unlock()
	return nil
}

func newTestDispatcher(p Policy) (*Dispatcher, *sink, *fakeClock) {
	s := &sink{}
	c := &fakeClock{t: time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)}
	// Window > 0 schedules a real timer; tests flush manually instead.
	d := NewDispatcher(s.send, func(context.Context) Policy { return p })
	d.Now = c.now
	return d, s, c
}

func note(id int64, name, kind, host string, at time.Time) monitor.Notification {
	return monitor.Notification{Kind: kind, Monitor: monitor.Config{ID: id, Name: name, Kind: "http", HostName: host, Priority: 1}, Message: "timed out", At: at}
}

func TestGroupsBurstIntoOneMessage(t *testing.T) {
	d, s, c := newTestDispatcher(Policy{Window: time.Hour, MaxPerHour: 20})
	ctx := context.Background()
	for i, name := range []string{"Jellyfin", "Radarr", "Sonarr"} {
		d.Notify(ctx, note(int64(i+1), name, "down", "docker-media", c.t))
	}
	d.Notify(ctx, note(9, "Pi-hole", "down", "", c.t))
	d.Flush(ctx)
	if len(s.msgs) != 1 {
		t.Fatalf("want 1 grouped message, got %d", len(s.msgs))
	}
	m := s.msgs[0]
	if m.Title != "🔴 4 down" || !strings.Contains(m.Body, "Likely cause: <b>docker-media</b> (3 of its services are down)") || m.Priority != 1 {
		t.Fatalf("grouped message wrong: %+v", m)
	}
}

func TestBlipCollapses(t *testing.T) {
	d, s, c := newTestDispatcher(Policy{Window: time.Hour})
	ctx := context.Background()
	d.Notify(ctx, note(1, "Jellyfin", "down", "", c.t))
	up := note(1, "Jellyfin", "up", "", c.t.Add(40*time.Second))
	up.DownSince = c.t
	d.Notify(ctx, up)
	d.Flush(ctx)
	if len(s.msgs) != 1 || !strings.Contains(s.msgs[0].Title, "short outage") || !strings.Contains(s.msgs[0].Body, "40s") || s.msgs[0].Priority != 0 {
		t.Fatalf("blip: %+v", s.msgs)
	}
}

func TestSingleUsesClassicFormat(t *testing.T) {
	d, s, c := newTestDispatcher(Policy{Window: 0})
	d.Notify(context.Background(), note(1, "Jellyfin", "down", "", c.t))
	if len(s.msgs) != 1 || s.msgs[0].Title != "🔴 Jellyfin is down" {
		t.Fatalf("single: %+v", s.msgs)
	}
}

func TestHourlyCap(t *testing.T) {
	d, s, c := newTestDispatcher(Policy{Window: 0, MaxPerHour: 2})
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		d.Notify(ctx, note(int64(i), "svc", "down", "", c.t))
	}
	// 2 allowed, then a cap notice; alerts 3–5 are held back.
	if len(s.msgs) != 3 || !strings.Contains(s.msgs[2].Title, "limit reached") {
		t.Fatalf("cap: %d msgs, last %+v", len(s.msgs), s.msgs[len(s.msgs)-1])
	}
	c.t = c.t.Add(61 * time.Minute)
	d.Notify(ctx, note(10, "later", "down", "", c.t))
	last := s.msgs[len(s.msgs)-1]
	if !strings.Contains(last.Body, "+3 earlier alerts held back") {
		t.Fatalf("expected held-back count, got %+v", last)
	}
}

func TestFlapProtection(t *testing.T) {
	d, s, c := newTestDispatcher(Policy{Window: 0, FlapProtection: true})
	ctx := context.Background()
	kinds := []string{"down", "up", "down", "up", "down", "up", "down", "up"}
	for _, k := range kinds {
		c.t = c.t.Add(2 * time.Minute)
		d.Notify(ctx, note(1, "Wobbly", k, "", c.t))
	}
	// 5 normal alerts, then one "flapping" notice, then silence.
	if len(s.msgs) != 6 || !strings.Contains(s.msgs[5].Title, "flapping") {
		t.Fatalf("flap: %d msgs: %+v", len(s.msgs), s.msgs[len(s.msgs)-1])
	}
	c.t = c.t.Add(16 * time.Minute)
	d.CheckFlaps(ctx)
	last := s.msgs[len(s.msgs)-1]
	if len(s.msgs) != 7 || !strings.Contains(last.Title, "stopped flapping") || !strings.Contains(last.Body, "now <b>up</b>") {
		t.Fatalf("settle: %+v", last)
	}
}
