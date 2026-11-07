// Package glinet talks to GL.iNet routers running firmware 4.x over their
// JSON-RPC API (/rpc): DHCP address reservations and the client list.
//
// Login is a challenge/response: the router returns a salt + nonce, the
// password is crypt()ed with that salt (md5/sha256/sha512-crypt, like
// `openssl passwd -1/-5/-6`), then hash("user:crypt:nonce") is sent to get a
// session id. Every later call is {"method":"call","params":[sid, module,
// method, args]}.
//
// lan.remove_static_bind doesn't work through the API on current firmware,
// so LabDash only lists and adds reservations.
package glinet

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GehirnInc/crypt"
	_ "github.com/GehirnInc/crypt/md5_crypt"
	_ "github.com/GehirnInc/crypt/sha256_crypt"
	_ "github.com/GehirnInc/crypt/sha512_crypt"
)

// Secret is stored encrypted.
type Secret struct {
	Password string `json:"password"`
}

type Config struct {
	URL      string `json:"url"`      // http://192.168.8.1
	Username string `json:"username"` // usually root
}

type StaticBind struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
	IP   string `json:"ip"`
}

type ClientInfo struct {
	MAC       string `json:"mac"`
	IP        string `json:"ip"`
	Name      string `json:"name"`
	Alias     string `json:"alias"`
	Online    bool   `json:"online"`
	Interface string `json:"iface"`
	Remote    bool   `json:"remote"`
}

// DisplayName prefers the alias set in the router UI.
func (c ClientInfo) DisplayName() string {
	if c.Alias != "" {
		return c.Alias
	}
	return c.Name
}

// RPCError is an error returned by the router.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	switch e.Code {
	case -32000:
		return "router rejected the session or password"
	case -32003:
		return "router locked out logins after too many failures; wait a few minutes"
	case -32601:
		return "this router firmware doesn't support that API call"
	}
	return fmt.Sprintf("router error %d: %s", e.Code, e.Message)
}

type Client struct {
	mu   sync.Mutex
	sid  string
	key  string // config identity the sid belongs to
	HTTP *http.Client
}

func New() *Client {
	return &Client{HTTP: &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // routers use self-signed certs
	}}
}

func endpoint(cfg Config) (string, error) {
	u := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", errors.New("router URL must look like http://192.168.8.1")
	}
	return strings.TrimSuffix(u, "/rpc") + "/rpc", nil
}

func (c *Client) rpc(ctx context.Context, cfg Config, method string, params any, out any) error {
	ep, err := endpoint(cfg)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("can't reach the router: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("router: %s", resp.Status)
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("unexpected router response: %w", err)
	}
	if env.Error != nil {
		return env.Error
	}
	if out != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// CryptPassword reproduces `openssl passwd -<alg> -salt <salt> <password>`.
func CryptPassword(alg int, salt, password string) (string, error) {
	var c crypt.Crypter
	var prefix string
	switch alg {
	case 1:
		c, prefix = crypt.MD5.New(), "$1$"
	case 5:
		c, prefix = crypt.SHA256.New(), "$5$"
	case 6:
		c, prefix = crypt.SHA512.New(), "$6$"
	default:
		return "", fmt.Errorf("router asked for unsupported password algorithm %d", alg)
	}
	return c.Generate([]byte(password), []byte(prefix+salt))
}

// LoginHash combines user, crypted password and nonce as the router expects.
func LoginHash(method, username, cipher, nonce string) (string, error) {
	data := []byte(username + ":" + cipher + ":" + nonce)
	switch strings.ToLower(method) {
	case "", "md5":
		h := md5.Sum(data)
		return hex.EncodeToString(h[:]), nil
	case "sha256":
		h := sha256.Sum256(data)
		return hex.EncodeToString(h[:]), nil
	case "sha512":
		h := sha512.Sum512(data)
		return hex.EncodeToString(h[:]), nil
	}
	return "", fmt.Errorf("router asked for unsupported hash method %q", method)
}

func (c *Client) login(ctx context.Context, cfg Config, sec Secret) (string, error) {
	user := cfg.Username
	if user == "" {
		user = "root"
	}
	if sec.Password == "" {
		return "", errors.New("router password is required")
	}
	var ch struct {
		Alg        int    `json:"alg"`
		Salt       string `json:"salt"`
		Nonce      string `json:"nonce"`
		HashMethod string `json:"hash-method"`
	}
	if err := c.rpc(ctx, cfg, "challenge", map[string]string{"username": user}, &ch); err != nil {
		return "", err
	}
	cipher, err := CryptPassword(ch.Alg, ch.Salt, sec.Password)
	if err != nil {
		return "", err
	}
	hash, err := LoginHash(ch.HashMethod, user, cipher, ch.Nonce)
	if err != nil {
		return "", err
	}
	var res struct {
		SID string `json:"sid"`
	}
	if err := c.rpc(ctx, cfg, "login", map[string]string{"username": user, "hash": hash}, &res); err != nil {
		var re *RPCError
		if errors.As(err, &re) && re.Code == -32000 {
			return "", errors.New("router login failed: wrong username or password")
		}
		return "", err
	}
	if res.SID == "" {
		return "", errors.New("router login returned no session")
	}
	return res.SID, nil
}

// Call runs module.method, logging in (or back in, if the session expired).
func (c *Client) Call(ctx context.Context, cfg Config, sec Secret, module, method string, args, out any) error {
	if args == nil {
		args = map[string]any{}
	}
	key := cfg.URL + "|" + cfg.Username + "|" + sec.Password
	for attempt := 0; attempt < 2; attempt++ {
		c.mu.Lock()
		sid := ""
		if c.key == key {
			sid = c.sid
		}
		c.mu.Unlock()
		if sid == "" {
			var err error
			if sid, err = c.login(ctx, cfg, sec); err != nil {
				return err
			}
			c.mu.Lock()
			c.sid, c.key = sid, key
			c.mu.Unlock()
		}
		err := c.rpc(ctx, cfg, "call", []any{sid, module, method, args}, out)
		var re *RPCError
		if errors.As(err, &re) && re.Code == -32000 && attempt == 0 {
			c.mu.Lock()
			c.sid = "" // session expired: log in again once
			c.mu.Unlock()
			continue
		}
		return err
	}
	return nil
}

func (c *Client) StaticBinds(ctx context.Context, cfg Config, sec Secret) ([]StaticBind, error) {
	var out struct {
		List []StaticBind `json:"static_bind_list"`
	}
	err := c.Call(ctx, cfg, sec, "lan", "get_static_bind_list", nil, &out)
	for i := range out.List {
		out.List[i].MAC = strings.ToLower(out.List[i].MAC)
	}
	return out.List, err
}

func (c *Client) Clients(ctx context.Context, cfg Config, sec Secret) ([]ClientInfo, error) {
	var out struct {
		Clients []ClientInfo `json:"clients"`
	}
	err := c.Call(ctx, cfg, sec, "clients", "get_list", nil, &out)
	for i := range out.Clients {
		out.Clients[i].MAC = strings.ToLower(out.Clients[i].MAC)
	}
	return out.Clients, err
}

// AddStaticBind reserves ip for mac on the router.
func (c *Client) AddStaticBind(ctx context.Context, cfg Config, sec Secret, b StaticBind) error {
	hw, err := net.ParseMAC(strings.TrimSpace(b.MAC))
	if err != nil || len(hw) != 6 {
		return fmt.Errorf("invalid MAC address %q", b.MAC)
	}
	if ip := net.ParseIP(strings.TrimSpace(b.IP)); ip == nil || ip.To4() == nil {
		return fmt.Errorf("invalid IPv4 address %q", b.IP)
	}
	b.MAC = strings.ToUpper(hw.String())
	b.IP = strings.TrimSpace(b.IP)
	b.Name = strings.TrimSpace(b.Name)
	return c.Call(ctx, cfg, sec, "lan", "add_static_bind", b, nil)
}
