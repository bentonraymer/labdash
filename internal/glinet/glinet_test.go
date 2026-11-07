package glinet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Vectors from: openssl passwd -1/-5/-6 -salt Ab3dEf9h hunter2-router
func TestCryptMatchesOpenSSL(t *testing.T) {
	want := map[int]string{
		1: "$1$Ab3dEf9h$vrDj0/9uxTPaUP2LLJDju.",
		5: "$5$Ab3dEf9h$AObXq1HmSGGuLfRpxvArNoRk29fBbzE1mFcJDP.8To9",
		6: "$6$Ab3dEf9h$xRhJGJ0ctjrIAT6mIoBLlqEE2hE4iT5kwy3NXJqOVEbaevB5KjUAtmMdeGO.yDKLWiE0jTRfMj1o/W2zl66GQ/",
	}
	for alg, w := range want {
		got, err := CryptPassword(alg, "Ab3dEf9h", "hunter2-router")
		if err != nil || got != w {
			t.Errorf("alg %d: got %q (%v) want %q", alg, got, err, w)
		}
	}
	if _, err := CryptPassword(9, "x", "y"); err == nil {
		t.Error("unknown alg should fail")
	}
}

// fakeRouter implements just enough of the GL.iNet 4.x RPC API.
func fakeRouter(t *testing.T, expireAfter int32) (*httptest.Server, *[]map[string]string, *atomic.Int32) {
	var calls atomic.Int32
	added := []map[string]string{}
	cipher, _ := CryptPassword(5, "s4lt", "pw")
	goodHash, _ := LoginHash("md5", "root", cipher, "n0nce")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		reply := func(v any) { json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": v}) }
		fail := func(code int) {
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": code, "message": "Access denied"}})
		}
		switch req.Method {
		case "challenge":
			reply(map[string]any{"alg": 5, "salt": "s4lt", "nonce": "n0nce", "hash-method": "md5"})
		case "login":
			var p map[string]string
			json.Unmarshal(req.Params, &p)
			if p["hash"] != goodHash {
				fail(-32000)
				return
			}
			reply(map[string]string{"sid": "SID1"})
		case "call":
			var p []json.RawMessage
			json.Unmarshal(req.Params, &p)
			var sid, module, method string
			json.Unmarshal(p[0], &sid)
			json.Unmarshal(p[1], &module)
			json.Unmarshal(p[2], &method)
			n := calls.Add(1)
			if sid != "SID1" || (expireAfter > 0 && n == expireAfter) {
				fail(-32000) // simulate an expired session once
				return
			}
			switch module + "." + method {
			case "lan.get_static_bind_list":
				reply(map[string]any{"static_bind_list": []map[string]string{{"name": "nas", "mac": "AA:BB:CC:00:00:02", "ip": "192.168.8.2"}}})
			case "clients.get_list":
				reply(map[string]any{"clients": []map[string]any{{"mac": "AA:BB:CC:00:00:09", "ip": "192.168.9.25", "name": "android-tv", "alias": "Living room TV", "online": true}}})
			case "lan.add_static_bind":
				var b map[string]string
				json.Unmarshal(p[3], &b)
				added = append(added, b)
				reply([]any{})
			default:
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32601, "message": "Method not found"}})
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &added, &calls
}

func TestClientFlow(t *testing.T) {
	srv, added, _ := fakeRouter(t, 2) // 2nd call hits an "expired" session
	c := New()
	ctx := context.Background()
	cfg := Config{URL: srv.URL, Username: "root"}
	sec := Secret{Password: "pw"}

	binds, err := c.StaticBinds(ctx, cfg, sec)
	if err != nil || len(binds) != 1 || binds[0].MAC != "aa:bb:cc:00:00:02" {
		t.Fatalf("binds: %+v %v", binds, err)
	}
	clients, err := c.Clients(ctx, cfg, sec) // re-logs in transparently
	if err != nil || len(clients) != 1 || clients[0].DisplayName() != "Living room TV" {
		t.Fatalf("clients: %+v %v", clients, err)
	}
	if err := c.AddStaticBind(ctx, cfg, sec, StaticBind{Name: "grafana", MAC: "bc-24-11-0a-0b-0c", IP: "192.168.8.3"}); err != nil {
		t.Fatal(err)
	}
	if len(*added) != 1 || (*added)[0]["mac"] != "BC:24:11:0A:0B:0C" || (*added)[0]["ip"] != "192.168.8.3" {
		t.Fatalf("added: %+v", *added)
	}
	if err := c.AddStaticBind(ctx, cfg, sec, StaticBind{MAC: "nope", IP: "192.168.8.3"}); err == nil {
		t.Fatal("bad MAC should fail")
	}
	if _, err := New().StaticBinds(ctx, cfg, Secret{Password: "wrong"}); err == nil || err.Error() != "router login failed: wrong username or password" {
		t.Fatalf("wrong password: %v", err)
	}
}
