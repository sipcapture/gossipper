package settingsauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("settings migrate: %w", err)
		}
	}
	return nil
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
