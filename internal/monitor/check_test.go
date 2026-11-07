package monitor

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusMatches(t *testing.T) {
	cases := []struct {
		code int
		spec string
		want bool
	}{
		{200, "", true}, {302, "", true}, {404, "", false},
		{401, "200-399,401", true}, {403, "200-399,401", false},
		{503, "5xx", true}, {204, "2xx", true}, {418, " 418 ", true},
	}
	for _, c := range cases {
		if got := StatusMatches(c.code, c.spec); got != c.want {
			t.Errorf("StatusMatches(%d, %q) = %v", c.code, c.spec, got)
		}
	}
}

func TestCheckHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(500)
		}
		w.Write([]byte("hello jellyfin"))
	}))
	defer srv.Close()
	ctx := context.Background()

	if r := Check(ctx, CheckSpec{Kind: "http", Target: srv.URL}); !r.Up {
		t.Errorf("expected up: %+v", r)
	}
	if r := Check(ctx, CheckSpec{Kind: "http", Target: srv.URL + "/fail"}); r.Up || !strings.Contains(r.Message, "500") {
		t.Errorf("expected down with 500: %+v", r)
	}
	if r := Check(ctx, CheckSpec{Kind: "http", Target: srv.URL, Keyword: "jellyfin"}); !r.Up {
		t.Errorf("keyword should match: %+v", r)
	}
	if r := Check(ctx, CheckSpec{Kind: "http", Target: srv.URL, Keyword: "plex"}); r.Up {
		t.Errorf("keyword should not match: %+v", r)
	}
}

func TestCheckTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if r := Check(context.Background(), CheckSpec{Kind: "tcp", Target: addr}); !r.Up {
		t.Errorf("expected open: %+v", r)
	}
	ln.Close()
	if r := Check(context.Background(), CheckSpec{Kind: "tcp", Target: addr, Timeout: time.Second}); r.Up {
		t.Errorf("expected closed: %+v", r)
	}
}

func TestDeriveTarget(t *testing.T) {
	p := 8096
	st := ServiceTarget{DNSURL: "https://jf.home.example.com", DirectIP: "192.168.8.30", DirectPort: &p}
	cases := map[string][2]string{ // kind → {dns mode, direct mode}
		"http": {"https://jf.home.example.com", "http://192.168.8.30:8096"},
		"tcp":  {"192.168.8.30:8096", "192.168.8.30:8096"},
		"ping": {"192.168.8.30", "192.168.8.30"},
		"dns":  {"jf.home.example.com", "jf.home.example.com"},
	}
	for kind, want := range cases {
		if got := DeriveTarget(kind, st, "dns"); got != want[0] {
			t.Errorf("%s/dns = %q want %q", kind, got, want[0])
		}
		if got := DeriveTarget(kind, st, "direct"); got != want[1] {
			t.Errorf("%s/direct = %q want %q", kind, got, want[1])
		}
	}
	if got := DeriveTarget("tcp", ServiceTarget{DNSURL: "https://a.example.com"}, "dns"); got != "a.example.com:443" {
		t.Errorf("tcp falls back to dns host:443, got %q", got)
	}
}
