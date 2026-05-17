package settingsauth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "modernc.org/sqlite" // SQLite driver (pure Go)
)

const (
	jwtIssuer   = "gossipper"
	jwtTTL      = 24 * time.Hour
	kvJWTSecret = "jwt_secret"
)

// Auth implements internal JWT auth backed by SQLite users.
type Auth struct {
	db       *sql.DB
	secretMu sync.RWMutex
	secret   []byte
}

// Open opens the settings DB and returns an Auth service.
//
// Secret resolution order:
//  1. A value persisted in kv_settings.jwt_secret (set by a previous RotateSecret
//     call). When present this wins so rotations survive process restarts.
//  2. The jwtSecret argument (must be ≥ 16 chars). If 1 was empty but the
//     argument is valid, the argument is also persisted to kv_settings so
//     restarts without the flag keep working.
//
// Returns an error when neither source produces a valid secret.
func Open(sqlitePath, jwtSecret string) (*Auth, error) {
	db, err := OpenStore(sqlitePath)
	if err != nil {
		return nil, err
	}
	persisted, err := GetSetting(context.Background(), db, kvJWTSecret)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read jwt secret: %w", err)
	}
	jwtSecret = strings.TrimSpace(jwtSecret)
	secret := strings.TrimSpace(persisted)
	if secret == "" {
		secret = jwtSecret
	}
	if len(secret) < 16 {
		_ = db.Close()
		return nil, errors.New("jwt secret must be at least 16 characters (set --auth-jwt-secret or rotate via API)")
	}
	if persisted == "" {
		if err := PutSetting(context.Background(), db, kvJWTSecret, secret); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("persist initial jwt secret: %w", err)
		}
	}
	return &Auth{db: db, secret: []byte(secret)}, nil
}

// RotateSecret generates a fresh 256-bit JWT secret, persists it to
// kv_settings, swaps the in-memory key, and returns the new secret as a
// hex string for the caller to display once. All previously-issued tokens
// become invalid immediately.
func (a *Auth) RotateSecret(ctx context.Context) (string, error) {
	if !a.Enabled() {
		return "", errors.New("auth disabled")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rotate jwt secret: %w", err)
	}
	hexSecret := hex.EncodeToString(buf)
	if err := PutSetting(ctx, a.db, kvJWTSecret, hexSecret); err != nil {
		return "", err
	}
	a.secretMu.Lock()
	a.secret = []byte(hexSecret)
	a.secretMu.Unlock()
	return hexSecret, nil
}

func (a *Auth) signingKey() []byte {
	a.secretMu.RLock()
	defer a.secretMu.RUnlock()
	return a.secret
}

// Enabled is true when Auth was constructed successfully (caller only constructs when internal auth is on).
func (a *Auth) Enabled() bool {
	if a == nil || a.db == nil {
		return false
	}
	return len(a.signingKey()) >= 16
}

// Close releases the database handle.
func (a *Auth) Close() error {
	if a == nil || a.db == nil {
		return nil
	}
	return a.db.Close()
}

// Login validates credentials and returns a signed JWT (HS256) and expiry (Unix seconds).
func (a *Auth) Login(ctx context.Context, username, password string) (token string, expUnix int64, err error) {
	if !a.Enabled() {
		return "", 0, errors.New("auth disabled")
	}
	uid, err := VerifyUser(ctx, a.db, username, password)
	if err != nil {
		return "", 0, err
	}
	now := time.Now()
	exp := now.Add(jwtTTL)
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": jwtIssuer,
		"sub": strings.TrimSpace(username),
		"uid": uid,
		"iat": now.Unix(),
		"exp": exp.Unix(),
	}).SignedString(a.signingKey())
	if err != nil {
		return "", 0, err
	}
	return tok, exp.Unix(), nil
}

// ValidRequest reports whether the request carries a valid JWT (Authorization: Bearer or ?token=).
func (a *Auth) ValidRequest(r *http.Request) bool {
	if !a.Enabled() {
		return false
	}
	tok := bearerFromRequest(r)
	if tok == "" {
		return false
	}
	_, err := a.parseJWT(tok)
	return err == nil
}

func bearerFromRequest(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

func (a *Auth) parseJWT(token string) (jwt.MapClaims, error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return a.signingKey(), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	mc, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}
	if iss, _ := mc["iss"].(string); iss != jwtIssuer {
		return nil, errors.New("bad issuer")
	}
	return mc, nil
}

// CreateUser is a convenience for CLI bootstrap.
func (a *Auth) CreateUser(ctx context.Context, username, password string) error {
	if !a.Enabled() {
		return errors.New("auth disabled")
	}
	return CreateUser(ctx, a.db, username, password)
}

// ParseToken validates the JWT and returns its claims (jwt.MapClaims).
// Callers that only need to check validity should use ValidRequest.
func (a *Auth) ParseToken(token string) (jwt.Claims, error) {
	if !a.Enabled() {
		return nil, errors.New("auth disabled")
	}
	mc, err := a.parseJWT(strings.TrimSpace(token))
	if err != nil {
		return nil, err
	}
	return mc, nil
}

// DB returns the underlying SQLite handle (read-write). Callers must NOT close
// it. Returns nil when Auth is not enabled.
func (a *Auth) DB() *sql.DB {
	if a == nil {
		return nil
	}
	return a.db
}
