package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bentonraymer/labdash/internal/monitor"
	"github.com/bentonraymer/labdash/internal/pushover"
)

// Policy controls how notifications are grouped and throttled.
type Policy struct {
	Window         time.Duration // collect notifications this long before sending (0 = immediately)
	MaxPerHour     int           // 0 = unlimited
	FlapProtection bool
	BaseURL        string
}

const (
	flapWindow    = 30 * time.Minute
	flapThreshold = 6 // state changes within flapWindow (3 down/up cycles)
	flapCalm      = 15 * time.Minute
)

type flapState struct {
	changes   []time.Time
	flapping  bool
	last      monitor.Notification
	lastAt    time.Time
	announced bool
}

// Dispatcher sits between the monitor engine and Pushover. It batches
// notifications that arrive close together into one message, collapses
// quick down→up blips, points at a shared host when several of its services
// fail at once, enforces an hourly cap and mutes flapping monitors.
type Dispatcher struct {
	Send   func(ctx context.Context, m pushover.Message) error
	Policy func(ctx context.Context) Policy
	Now    func() time.Time

	mu         sync.Mutex
	pending    []monitor.Notification
	timer      *time.Timer
	sent       []time.Time
	suppressed int
	capNotice  bool
	flaps      map[int64]*flapState
}

func NewDispatcher(send func(context.Context, pushover.Message) error, policy func(context.Context) Policy) *Dispatcher {
	return &Dispatcher{Send: send, Policy: policy, Now: time.Now, flaps: map[int64]*flapState{}}
}

// Notify implements monitor.Notifier.
func (d *Dispatcher) Notify(ctx context.Context, n monitor.Notification) error {
	p := d.Policy(ctx)
	d.mu.Lock()
	if !(p.FlapProtection && d.trackFlap(n)) {
		d.pending = append(d.pending, n)
	}
	// (When flapping starts, trackFlap queued a single "flapping" notice.)
	if len(d.pending) == 0 {
		d.mu.Unlock()
		return nil
	}
	if p.Window <= 0 {
		d.mu.Unlock()
		d.Flush(context.Background())
		return nil
	}
	if d.timer == nil {
		d.timer = time.AfterFunc(p.Window, func() { d.Flush(context.Background()) })
	}
	d.mu.Unlock()
	return nil
}

// trackFlap records a state change and reports whether to suppress it.
// Caller holds d.mu.
func (d *Dispatcher) trackFlap(n monitor.Notification) bool {
	now := d.Now()
	f := d.flaps[n.Monitor.ID]
	if f == nil {
		f = &flapState{}
		d.flaps[n.Monitor.ID] = f
	}
	kept := f.changes[:0]
	for _, t := range f.changes {
		if now.Sub(t) < flapWindow {
			kept = append(kept, t)
		}
	}
	f.changes = append(kept, now)
	f.last, f.lastAt = n, now
	if f.flapping {
		return true
	}
	if len(f.changes) >= flapThreshold {
		f.flapping = true
		f.announced = false
		// Replace this change with a single "flapping" notice.
		flap := n
		flap.Kind = "flapping"
		d.pending = append(d.pending, flap)
		return true
	}
	return false
}

// CheckFlaps ends flapping once a monitor has been quiet for flapCalm and
// reports its settled state. Call periodically.
func (d *Dispatcher) CheckFlaps(ctx context.Context) {
	now := d.Now()
	d.mu.Lock()
	var settled []monitor.Notification
	for id, f := range d.flaps {
		if f.flapping && now.Sub(f.lastAt) >= flapCalm {
			f.flapping = false
			f.changes = nil
			n := f.last
			n.Kind = "settled:" + n.Kind
			settled = append(settled, n)
		}
		if !f.flapping && len(f.changes) == 0 {
			delete(d.flaps, id)
		}
	}
	d.pending = append(d.pending, settled...)
	d.mu.Unlock()
	if len(settled) > 0 {
		d.Flush(ctx)
	}
}

// Run drives CheckFlaps until ctx ends.
func (d *Dispatcher) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.CheckFlaps(ctx)
		}
	}
}

// Flush sends everything pending as one (grouped) message.
func (d *Dispatcher) Flush(ctx context.Context) {
	d.mu.Lock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	batch := d.pending
	d.pending = nil
	d.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	p := d.Policy(ctx)
	msg, count := Group(batch, p.BaseURL)
	d.deliver(ctx, p, msg, count)
}

// SendDirect sends a non-monitor message (e.g. new devices) through the
// hourly cap.
func (d *Dispatcher) SendDirect(ctx context.Context, m pushover.Message) {
	d.deliver(ctx, d.Policy(ctx), m, 1)
}

func (d *Dispatcher) deliver(ctx context.Context, p Policy, m pushover.Message, count int) {
	now := d.Now()
	d.mu.Lock()
	kept := d.sent[:0]
	for _, t := range d.sent {
		if now.Sub(t) < time.Hour {
			kept = append(kept, t)
		}
	}
	d.sent = kept
	if p.MaxPerHour > 0 && len(d.sent) >= p.MaxPerHour {
		d.suppressed += count
		notice := !d.capNotice
		d.capNotice = true
		d.mu.Unlock()
		if notice {
			// One heads-up, sent even though we're at the cap.
			cap := pushover.Message{
				Title:    "⚠️ LabDash alert limit reached",
				Body:     fmt.Sprintf("More than %d alerts in the last hour. Further alerts are held back until the rate drops; check LabDash for the full picture.", p.MaxPerHour),
				Priority: 0,
			}
			if p.BaseURL != "" {
				cap.URL, cap.URLTitle = strings.TrimRight(p.BaseURL, "/")+"/status", "Open LabDash status"
			}
			d.send(ctx, cap)
		}
		return
	}
	if d.suppressed > 0 {
		m.Body += fmt.Sprintf("\n<i>+%d earlier alert%s held back by the hourly limit.</i>", d.suppressed, plural(d.suppressed))
		d.suppressed = 0
	}
	d.capNotice = false
	d.sent = append(d.sent, now)
	d.mu.Unlock()
	d.send(ctx, m)
}

func (d *Dispatcher) send(ctx context.Context, m pushover.Message) {
	if err := d.Send(ctx, m); err != nil {
		slog.Warn("pushover send failed", "err", err)
	}
}

// ── grouping ──────────────────────────────────────────────────────────────

type item struct {
	kind     string // down | up | blip | flapping | settled-up | settled-down
	n        monitor.Notification
	downFor  time.Duration
	priority int
}

// Group turns a batch into one message. It returns the message and how many
// alert items it contains.
func Group(batch []monitor.Notification, baseURL string) (pushover.Message, int) {
	// Collapse per monitor, keeping arrival order.
	byID := map[int64]*item{}
	var order []int64
	for _, n := range batch {
		id := n.Monitor.ID
		cur, seen := byID[id]
		if !seen {
			order = append(order, id)
		}
		switch {
		case n.Kind == "flapping":
			byID[id] = &item{kind: "flapping", n: n}
		case strings.HasPrefix(n.Kind, "settled:"):
			byID[id] = &item{kind: "settled-" + strings.TrimPrefix(n.Kind, "settled:"), n: n}
		case seen && cur.kind == "down" && n.Kind == "up":
			// Went down and came back inside the window: a blip.
			dur := n.At.Sub(cur.n.At)
			if !n.DownSince.IsZero() {
				dur = n.At.Sub(n.DownSince)
			}
			byID[id] = &item{kind: "blip", n: n, downFor: dur}
		default:
			byID[id] = &item{kind: n.Kind, n: n}
		}
	}
	items := make([]*item, 0, len(order))
	for _, id := range order {
		it := byID[id]
		it.priority = it.n.Monitor.Priority
		if it.kind != "down" {
			it.priority = min(it.priority, 0)
		}
		items = append(items, it)
	}

	m := pushover.Message{HTML: true}
	if baseURL != "" {
		m.URL, m.URLTitle = strings.TrimRight(baseURL, "/")+"/status", "Open LabDash status"
	}

	if len(items) == 1 {
		it := items[0]
		switch it.kind {
		case "down", "up":
			msg := Format(it.n, baseURL)
			return msg, 1
		case "blip":
			m.Title = "🟠 " + it.n.Monitor.DisplayName() + " had a short outage"
			m.Body = fmt.Sprintf("<b>%s</b> was down for %s and is back up.", esc(it.n.Monitor.DisplayName()), HumanDuration(it.downFor))
		case "flapping":
			m.Title = "〰️ " + it.n.Monitor.DisplayName() + " is flapping"
			m.Body = fmt.Sprintf("<b>%s</b> changed state %d+ times in %s. Its alerts are muted until it's stable for %s.",
				esc(it.n.Monitor.DisplayName()), flapThreshold, HumanDuration(flapWindow), HumanDuration(flapCalm))
		default: // settled
			state := "up"
			if it.kind == "settled-down" {
				state = "down"
			}
			m.Title = "✅ " + it.n.Monitor.DisplayName() + " stopped flapping"
			m.Body = fmt.Sprintf("<b>%s</b> has been stable for %s and is now <b>%s</b>.", esc(it.n.Monitor.DisplayName()), HumanDuration(flapCalm), state)
		}
		m.Priority = it.priority
		m.Sound = it.n.Monitor.Sound
		return m, 1
	}

	// Several items: one grouped message.
	var downs, ups, blips, others []*item
	for _, it := range items {
		switch it.kind {
		case "down":
			downs = append(downs, it)
		case "up":
			ups = append(ups, it)
		case "blip":
			blips = append(blips, it)
		default:
			others = append(others, it)
		}
	}
	var title []string
	if len(downs) > 0 {
		title = append(title, fmt.Sprintf("🔴 %d down", len(downs)))
	}
	if len(ups) > 0 {
		title = append(title, fmt.Sprintf("🟢 %d recovered", len(ups)))
	}
	if len(blips) > 0 {
		title = append(title, fmt.Sprintf("🟠 %d brief outage%s", len(blips), plural(len(blips))))
	}
	if len(others) > 0 {
		title = append(title, fmt.Sprintf("〰️ %d flapping update%s", len(others), plural(len(others))))
	}
	m.Title = strings.Join(title, " · ")

	var b strings.Builder
	// Shared host: likely the real cause.
	hostDowns := map[string]int{}
	for _, it := range downs {
		if h := it.n.Monitor.HostName; h != "" {
			hostDowns[h]++
		}
	}
	var hosts []string
	for h, c := range hostDowns {
		if c >= 2 {
			hosts = append(hosts, h)
		}
	}
	sort.Strings(hosts)
	for _, h := range hosts {
		fmt.Fprintf(&b, "⚠️ Likely cause: <b>%s</b> (%d of its services are down)\n", esc(h), hostDowns[h])
	}
	line := func(it *item, suffix string) {
		fmt.Fprintf(&b, "• <b>%s</b>%s\n", esc(it.n.Monitor.DisplayName()), suffix)
	}
	if len(downs) > 0 {
		b.WriteString("<b>Down</b>\n")
		for _, it := range downs {
			line(it, " — "+esc(trimMsg(it.n.Message)))
		}
	}
	if len(blips) > 0 {
		b.WriteString("<b>Brief outages (now up)</b>\n")
		for _, it := range blips {
			line(it, " — down "+HumanDuration(it.downFor))
		}
	}
	if len(ups) > 0 {
		b.WriteString("<b>Recovered</b>\n")
		for _, it := range ups {
			suffix := ""
			if !it.n.DownSince.IsZero() {
				suffix = " after " + HumanDuration(it.n.At.Sub(it.n.DownSince))
			}
			line(it, suffix)
		}
	}
	for _, it := range others {
		if it.kind == "flapping" {
			line(it, " is flapping (alerts muted)")
		} else {
			line(it, " stopped flapping")
		}
	}
	m.Body = strings.TrimSpace(b.String())

	// Loudest member decides priority and sound.
	m.Priority = -2
	for _, it := range items {
		if it.priority > m.Priority {
			m.Priority = it.priority
			m.Sound = it.n.Monitor.Sound
		}
	}
	return m, len(items)
}

func trimMsg(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
