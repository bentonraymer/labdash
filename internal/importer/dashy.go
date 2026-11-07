// Package importer converts other dashboards' configs into LabDash
// sections and services.
package importer

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

type Section struct {
	Name      string    `json:"name"`
	Icon      string    `json:"icon"`
	Collapsed bool      `json:"collapsed"`
	Services  []Service `json:"services"`
}

type Service struct {
	Name         string `json:"name"`
	Icon         string `json:"icon"`
	Description  string `json:"description"`
	DNSURL       string `json:"dnsUrl"`
	DirectScheme string `json:"directScheme"`
	DirectIP     string `json:"directIp"`
	DirectPort   *int   `json:"directPort"`
	DirectPath   string `json:"directPath"`
	NewTab       bool   `json:"newTab"`
}

type dashyConf struct {
	PageInfo struct {
		Title string `yaml:"title"`
	} `yaml:"pageInfo"`
	Sections []dashySection `yaml:"sections"`
	// Multi-page configs keep sections under pages[].sections in sub-files;
	// only the root file is supported.
}

type dashySection struct {
	Name        string      `yaml:"name"`
	Icon        string      `yaml:"icon"`
	Items       []dashyItem `yaml:"items"`
	DisplayData struct {
		Collapsed bool `yaml:"collapsed"`
	} `yaml:"displayData"`
}

type dashyItem struct {
	Title       string      `yaml:"title"`
	Description string      `yaml:"description"`
	Icon        string      `yaml:"icon"`
	URL         string      `yaml:"url"`
	Target      string      `yaml:"target"`
	SubItems    []dashyItem `yaml:"subItems"`
}

// ParseDashy parses a Dashy conf.yml into LabDash sections.
func ParseDashy(data []byte) ([]Section, error) {
	var conf dashyConf
	if err := yaml.Unmarshal(data, &conf); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if len(conf.Sections) == 0 {
		return nil, errors.New("no sections found — is this a Dashy conf.yml?")
	}
	out := make([]Section, 0, len(conf.Sections))
	for _, ds := range conf.Sections {
		sec := Section{Name: strings.TrimSpace(ds.Name), Icon: mapIcon(ds.Icon), Collapsed: ds.DisplayData.Collapsed}
		if sec.Name == "" {
			sec.Name = "Imported"
		}
		for _, it := range flatten(ds.Items) {
			if strings.TrimSpace(it.Title) == "" {
				continue
			}
			sec.Services = append(sec.Services, convertItem(it))
		}
		out = append(out, sec)
	}
	return out, nil
}

// flatten turns Dashy "subItems" groups into individual services.
func flatten(items []dashyItem) []dashyItem {
	var out []dashyItem
	for _, it := range items {
		if len(it.SubItems) > 0 && it.URL == "" {
			out = append(out, flatten(it.SubItems)...)
			continue
		}
		out = append(out, it)
	}
	return out
}

func convertItem(it dashyItem) Service {
	s := Service{
		Name:         strings.TrimSpace(it.Title),
		Icon:         mapIcon(it.Icon),
		Description:  strings.TrimSpace(it.Description),
		DirectScheme: "http",
		NewTab:       it.Target == "" || it.Target == "newtab",
	}
	u, err := url.Parse(strings.TrimSpace(it.URL))
	if err != nil || u.Host == "" {
		return s
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil {
		// IP-based link → direct mode fields.
		s.DirectScheme = u.Scheme
		if s.DirectScheme != "https" {
			s.DirectScheme = "http"
		}
		s.DirectIP = host
		if p := u.Port(); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				s.DirectPort = &n
			}
		}
		path := u.EscapedPath()
		if u.RawQuery != "" {
			path += "?" + u.RawQuery
		}
		if u.Fragment != "" {
			path += "#" + u.Fragment
		}
		if path != "/" {
			s.DirectPath = path
		}
		return s
	}
	s.DNSURL = u.String()
	return s
}

// mapIcon converts Dashy icon references to LabDash's format.
//
//	hl-jellyfin / sh-jellyfin  → di:jellyfin (dashboard-icons)
//	https://…                  → kept as URL
//	favicon / generative / fa  → "" (auto)
//	🎬                         → emoji:🎬
func mapIcon(icon string) string {
	icon = strings.TrimSpace(icon)
	switch {
	case icon == "":
		return ""
	case strings.HasPrefix(icon, "hl-"), strings.HasPrefix(icon, "sh-"):
		name := strings.ToLower(icon[3:])
		name = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, ".svg"), ".png"), ".webp")
		return "di:" + name
	case strings.HasPrefix(icon, "http://"), strings.HasPrefix(icon, "https://"):
		return icon
	case utf8.RuneCountInString(icon) <= 4 && !isASCII(icon):
		return "emoji:" + icon
	default:
		return "" // favicon, generative, font-awesome, simple-icons: fall back to auto
	}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
