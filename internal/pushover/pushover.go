// Package pushover sends notifications through the Pushover API.
package pushover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const endpoint = "https://api.pushover.net/1/messages.json"

// Secret is stored encrypted in the integrations table.
type Secret struct {
	UserKey  string `json:"userKey"`
	AppToken string `json:"appToken"`
}

// Config is stored in plain JSON.
type Config struct {
	Device string `json:"device"` // optional: limit to one device name
	// BaseURL is LabDash's own URL, used for "open status page" links.
	BaseURL string `json:"baseUrl"`
}

type Message struct {
	Title    string
	Body     string
	Priority int // -2..2; 2 = emergency (repeats until acknowledged)
	Sound    string
	URL      string
	URLTitle string
	HTML     bool
}

type Client struct {
	HTTP     *http.Client
	Endpoint string
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second}, Endpoint: endpoint}
}

func (c *Client) Send(ctx context.Context, sec Secret, cfg Config, m Message) error {
	if sec.UserKey == "" || sec.AppToken == "" {
		return errors.New("pushover user key and app token are required")
	}
	v := url.Values{
		"token":   {sec.AppToken},
		"user":    {sec.UserKey},
		"message": {m.Body},
	}
	if m.Title != "" {
		v.Set("title", m.Title)
	}
	if m.Priority < -2 {
		m.Priority = -2
	}
	if m.Priority > 2 {
		m.Priority = 2
	}
	v.Set("priority", strconv.Itoa(m.Priority))
	if m.Priority == 2 {
		v.Set("retry", "60")
		v.Set("expire", "3600")
	}
	if m.Sound != "" {
		v.Set("sound", m.Sound)
	}
	if m.URL != "" {
		v.Set("url", m.URL)
		if m.URLTitle != "" {
			v.Set("url_title", m.URLTitle)
		}
	}
	if m.HTML {
		v.Set("html", "1")
	}
	if cfg.Device != "" {
		v.Set("device", cfg.Device)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		Status int      `json:"status"`
		Errors []string `json:"errors"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusOK || out.Status != 1 {
		if len(out.Errors) > 0 {
			return fmt.Errorf("pushover: %s", strings.Join(out.Errors, "; "))
		}
		return fmt.Errorf("pushover: %s", resp.Status)
	}
	return nil
}
