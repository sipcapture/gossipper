package settingsauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// OpenStore opens the SQLite settings database (creates file if missing) and runs migrations.
func OpenStore(path string) (*sql.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("settings sqlite path is empty")
	}
	if dir := filepath.Dir(path); dir != "." && dir != string(filepath.Separator) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("settings sqlite: mkdir %q: %w", dir, err)
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE COLLATE NOCASE,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);`,
		// Adds a role column (default 'admin') for future RBAC work without
		// touching legacy rows. Phase 5 will start consuming it.
		`ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'admin';`,
		// Jobs created by the UI (start-scenario / start-uac runs). The
		// supervisor reads/writes this table to track lifecycle.
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			profile_id TEXT,
			profile_kind TEXT NOT NULL DEFAULT '',
			scenario_id TEXT,
			status TEXT NOT NULL,
			args_json TEXT NOT NULL DEFAULT '{}',
			artifacts_dir TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			started_at TEXT,
			finished_at TEXT,
			exit_code INTEGER,
			error TEXT,
			created_by INTEGER,
			pid INTEGER
		);`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_profile ON jobs(profile_id);`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);`,
		// Files produced by a job (recordings, summary CSV, report HTML).
		`CREATE TABLE IF NOT EXISTS job_artifacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			path TEXT NOT NULL,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_job_artifacts_job ON job_artifacts(job_id);`,
		// Mutation log (admin actions: user CRUD, profile CRUD, job control).
		`CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts TEXT NOT NULL,
			user_id INTEGER,
			username TEXT,
			action TEXT NOT NULL,
			target TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_ts ON audit_log(ts);`,
		// kv_settings is a tiny key/value bag for runtime settings the admin
		// can rotate (e.g. jwt_secret). Values are opaque strings.
		`CREATE TABLE IF NOT EXISTS kv_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			if isDuplicateColumnErr(err) {
				continue
			}
			return fmt.Errorf("settings migrate: %w", err)
		}
	}
	return nil
}

// isDuplicateColumnErr reports whether err is the SQLite "duplicate column"
// error raised by idempotent ALTER TABLE … ADD COLUMN migrations.
func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column name")
}

// VerifyUser returns the numeric user id if password matches.
func VerifyUser(ctx context.Context, db *sql.DB, username, password string) (int64, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return 0, errors.New("missing credentials")
	}
	var id int64
	var hash string
	err := db.QueryRowContext(ctx, `SELECT id, password_hash FROM users WHERE username = ?`, username).Scan(&id, &hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$AAAAAAAAAAAAAAAAAAAAAA"), []byte(password))
			return 0, errors.New("invalid credentials")
		}
		return 0, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return 0, errors.New("invalid credentials")
	}
	return id, nil
}

// CreateUser inserts a user with a bcrypt-hashed password (or replaces password if username exists).
func CreateUser(ctx context.Context, db *sql.DB, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("username is required")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `
INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)
ON CONFLICT(username) DO UPDATE SET password_hash = excluded.password_hash`, username, string(hash), now)
	return err
}

// User describes a row in the users table.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// ListUsers returns all users ordered by id asc.
func ListUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, username, role, created_at FROM users ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetUser returns a user by id (sql.ErrNoRows when missing).
func GetUser(ctx context.Context, db *sql.DB, id int64) (User, error) {
	var u User
	err := db.QueryRowContext(ctx, `SELECT id, username, role, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt)
	return u, err
}

// UpdateUserRole flips the role column for an existing user.
func UpdateUserRole(ctx context.Context, db *sql.DB, id int64, role string) error {
	role = strings.TrimSpace(role)
	if role == "" {
		return errors.New("role is required")
	}
	res, err := db.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, role, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateUserPassword sets a new bcrypt-hashed password for the given user.
func UpdateUserPassword(ctx context.Context, db *sql.DB, id int64, password string) error {
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteUser removes a user row. The caller should prevent deleting the last
// remaining admin; this helper does not enforce that policy.
func DeleteUser(ctx context.Context, db *sql.DB, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AppendAudit writes a row to the audit_log table; never returns an error to
// avoid breaking mutating handlers when audit is non-critical.
func AppendAudit(ctx context.Context, db *sql.DB, userID *int64, username, action, target, payloadJSON string) {
	if strings.TrimSpace(payloadJSON) == "" {
		payloadJSON = "{}"
	}
	var uid any
	if userID != nil {
		uid = *userID
	}
	_, _ = db.ExecContext(ctx, `
INSERT INTO audit_log (ts, user_id, username, action, target, payload_json)
VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano), uid, strings.TrimSpace(username),
		strings.TrimSpace(action), strings.TrimSpace(target), payloadJSON)
}

// AuditEntry is a row from audit_log.
type AuditEntry struct {
	ID          int64  `json:"id"`
	TS          string `json:"ts"`
	UserID      *int64 `json:"user_id,omitempty"`
	Username    string `json:"username,omitempty"`
	Action      string `json:"action"`
	Target      string `json:"target"`
	PayloadJSON string `json:"payload_json,omitempty"`
}

// GetSetting reads a key from kv_settings. Returns ("", nil) when absent.
func GetSetting(ctx context.Context, db *sql.DB, key string) (string, error) {
	var v string
	err := db.QueryRowContext(ctx, `SELECT value FROM kv_settings WHERE key = ?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// PutSetting upserts a value into kv_settings.
func PutSetting(ctx context.Context, db *sql.DB, key, value string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO kv_settings(key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

// ListAudit returns the most recent audit rows first (limit defaults to 100).
func ListAudit(ctx context.Context, db *sql.DB, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, ts, user_id, username, action, target, payload_json FROM audit_log
ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var uid sql.NullInt64
		if err := rows.Scan(&e.ID, &e.TS, &uid, &e.Username, &e.Action, &e.Target, &e.PayloadJSON); err != nil {
			return nil, err
		}
		if uid.Valid {
			v := uid.Int64
			e.UserID = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
