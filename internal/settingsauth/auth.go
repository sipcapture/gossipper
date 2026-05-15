package settingsauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "modernc.org/sqlite" // SQLite driver (pure Go)
)

const jwtIssuer = "gossipper"
const jwtTTL = 24 * time.Hour

// Auth implements internal JWT auth backed by SQLite users.
type Auth struct {
	db     *sql.DB
	secret []byte
}

// Open opens the settings DB and returns an Auth service. jwtSecret must be at least 16 bytes.
func Open(sqlitePath, jwtSecret string) (*Auth, error) {
	jwtSecret = strings.TrimSpace(jwtSecret)
	if len(jwtSecret) < 16 {
		return nil, errors.New("jwt secret must be at least 16 characters")
	}
	db, err := OpenStore(sqlitePath)
	if err != nil {
		return nil, err
	}
	return &Auth{db: db, secret: []byte(jwtSecret)}, nil
}

// Enabled is true when Auth was constructed successfully (caller only constructs when internal auth is on).
func (a *Auth) Enabled() bool {
	return a != nil && a.db != nil && len(a.secret) >= 16
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
	}).SignedString(a.secret)
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
		return a.secret, nil
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
