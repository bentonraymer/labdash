package pushover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSend(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = map[string]string{}
		for k := range r.PostForm {
			got[k] = r.PostForm.Get(k)
		}
		if got["token"] == "bad" {
			w.WriteHeader(400)
			w.Write([]byte(`{"status":0,"errors":["application token is invalid"]}`))
			return
		}
		w.Write([]byte(`{"status":1}`))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), Endpoint: srv.URL}

	err := c.Send(context.Background(), Secret{UserKey: "u", AppToken: "t"}, Config{Device: "phone"},
		Message{Title: "Jellyfin down", Body: "timed out", Priority: 2, Sound: "siren", URL: "http://x/status", URLTitle: "Status"})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"user": "u", "token": "t", "priority": "2", "retry": "60", "expire": "3600", "sound": "siren", "device": "phone", "url_title": "Status"} {
		if got[k] != v {
			t.Errorf("%s = %q want %q", k, got[k], v)
		}
	}
	err = c.Send(context.Background(), Secret{UserKey: "u", AppToken: "bad"}, Config{}, Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected API error, got %v", err)
	}
}
