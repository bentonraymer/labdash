// Package auth implements the single local admin account and cookie sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	CookieName = "labdash_session"
	sessionTTL = 30 * 24 * time.Hour
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrRateLimited        = errors.New("too many attempts, try again shortly")
	ErrSetupDone          = errors.New("setup already completed")
)

var (
	dummyOnce sync.Once
	dummyHash []byte
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type Service struct {
	db      *sql.DB
	limiter *limiter
}

func New(db *sql.DB) *Service {
	return &Service{db: db, limiter: newLimiter(10, 5*time.Minute)}
}

func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n == 0, err
}

func (s *Service) CreateFirstUser(ctx context.Context, username, password string) (*User, error) {
	if err := validate(username, password); err != nil {
		return nil, err
	}
	need, err := s.NeedsSetup(ctx)
	if err != nil {
		return nil, err
	}
	if !need {
		return nil, ErrSetupDone
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO users(username, password_hash) VALUES (?, ?)`, username, string(hash))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, Username: username}, nil
}

func (s *Service) ChangePassword(ctx context.Context, userID int64, current, next string) error {
	var hash string
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return ErrInvalidCredentials
	}
	if err := validate("x", next); err != nil {
		return err
	}
	nh, err := bcrypt.GenerateFromPassword([]byte(next), 12)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, string(nh), userID)
	return err
}

func validate(username, password string) error {
	if strings.TrimSpace(username) == "" {
		return errors.New("username is required")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	return nil
}

// Login verifies credentials and returns a new session token.
func (s *Service) Login(ctx context.Context, ip, username, password, userAgent string) (string, *User, error) {
	if !s.limiter.allow(ip) {
		return "", nil, ErrRateLimited
	}
	var u User
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		// Spend similar time to avoid user enumeration.
		dummyOnce.Do(func() { dummyHash, _ = bcrypt.GenerateFromPassword([]byte("labdash-dummy"), 12) })
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", nil, ErrInvalidCredentials
	}
	if err != nil {
		return "", nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", nil, ErrInvalidCredentials
	}
	s.limiter.reset(ip)
	tok, err := s.NewSession(ctx, u.ID, userAgent)
	return tok, &u, err
}

func (s *Service) NewSession(ctx context.Context, userID int64, userAgent string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(buf)
	exp := time.Now().Add(sessionTTL).UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token_hash, user_id, user_agent, expires_at) VALUES (?, ?, ?, ?)`,
		hashToken(tok), userID, truncate(userAgent, 200), exp)
	return tok, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

func (s *Service) UserForToken(ctx context.Context, token string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`,
		hashToken(token), time.Now().UTC().Format(time.RFC3339)).Scan(&u.ID, &u.Username)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Service) PruneSessions(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC().Format(time.RFC3339))
}

// ── HTTP helpers ──────────────────────────────────────────────────────────

type ctxKey struct{}

func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

func UserFrom(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}

func SetCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: isHTTPS(r), MaxAge: -1})
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// Middleware attaches the user if the session cookie is valid and rejects the
// request with 401 otherwise.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil || c.Value == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		u, err := s.UserForToken(r.Context(), c.Value)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
	})
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ── login rate limiter ────────────────────────────────────────────────────

type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}
