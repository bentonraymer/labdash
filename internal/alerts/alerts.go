// Package alerts turns monitor transitions into Pushover notifications.
package alerts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bentonraymer/labdash/internal/integrations"
	"github.com/bentonraymer/labdash/internal/monitor"
	"github.com/bentonraymer/labdash/internal/pushover"
)

type Pushover struct {
	Store  *integrations.Store
	Client *pushover.Client
}

// Load returns the stored Pushover credentials if the integration is enabled.
func (p *Pushover) Load(ctx context.Context) (pushover.Secret, pushover.Config, error) {
	var sec pushover.Secret
	var cfg pushover.Config
	enabled, err := p.Store.Get(ctx, "pushover", &cfg, &sec)
	if err != nil {
		return sec, cfg, err
	}
	if !enabled {
		return sec, cfg, errors.New("pushover is disabled")
	}
	return sec, cfg, nil
}

func (p *Pushover) Notify(ctx context.Context, n monitor.Notification) error {
	sec, cfg, err := p.Load(ctx)
	if errors.Is(err, integrations.ErrNotConfigured) {
		return nil // nothing to send to
	}
	if err != nil {
		return err
	}
	return p.Client.Send(ctx, sec, cfg, Format(n, cfg.BaseURL))
}

// Format builds the Pushover message for a transition.
func Format(n monitor.Notification, baseURL string) pushover.Message {
	name := n.Monitor.DisplayName()
	m := pushover.Message{Sound: n.Monitor.Sound, HTML: true}
	if baseURL != "" {
		m.URL = strings.TrimRight(baseURL, "/") + "/status"
		m.URLTitle = "Open LabDash status"
	}
	switch n.Kind {
	case "down":
		m.Title = "🔴 " + name + " is down"
		m.Body = fmt.Sprintf("<b>%s</b> failed its %s check.\n%s\n<font color=\"#888888\">%s</font>",
			esc(name), strings.ToUpper(n.Monitor.Kind), esc(n.Message), esc(n.Monitor.Target))
		m.Priority = n.Monitor.Priority
	case "up":
		m.Title = "🟢 " + name + " is back up"
		body := fmt.Sprintf("<b>%s</b> is responding again.", esc(name))
		if !n.DownSince.IsZero() {
			body += " Down for " + HumanDuration(n.At.Sub(n.DownSince)) + "."
		}
		m.Body = body
		// Recovery is good news: never louder than normal.
		m.Priority = min(n.Monitor.Priority, 0)
	}
	return m
}

// HumanDuration renders 1h 5m / 3m 20s / 45s.
func HumanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
