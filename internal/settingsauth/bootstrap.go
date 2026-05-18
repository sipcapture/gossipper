package settingsauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	DefaultBootstrapUsername = "admin"
	DefaultBootstrapPassword = "sipcapture"
	kvBootstrapAt            = "bootstrap_at"
	kvAuthMode               = "auth_mode"
)

// EnsureBootstrap seeds a fresh settings DB with a default admin user and
// baseline kv_settings when no users exist yet. Existing deployments are
// left unchanged.
func EnsureBootstrap(ctx context.Context, db *sql.DB) (createdAdmin bool, err error) {
	if db == nil {
		return false, errors.New("settings db is nil")
	}
	users, err := ListUsers(ctx, db)
	if err != nil {
		return false, err
	}
	if len(users) > 0 {
		return false, nil
	}
	username := bootstrapUsername()
	password := bootstrapPassword()
	if err := CreateUser(ctx, db, username, password); err != nil {
		return false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := PutSetting(ctx, db, kvBootstrapAt, now); err != nil {
		return false, err
	}
	if err := PutSetting(ctx, db, kvAuthMode, "internal"); err != nil {
		return false, err
	}
	return true, nil
}

func bootstrapUsername() string {
	if u := strings.TrimSpace(os.Getenv("GOSSIPPER_BOOTSTRAP_USERNAME")); u != "" {
		return u
	}
	return DefaultBootstrapUsername
}

func bootstrapPassword() string {
	if p := strings.TrimSpace(os.Getenv("GOSSIPPER_BOOTSTRAP_PASSWORD")); len(p) >= 8 {
		return p
	}
	return DefaultBootstrapPassword
}

func logBootstrapCreated(username, password string) {
	fmt.Fprintf(os.Stderr, "auth: initialized settings DB with default admin user %q\n", username)
	if os.Getenv("GOSSIPPER_BOOTSTRAP_PASSWORD") == "" {
		fmt.Fprintf(os.Stderr, "auth: default password is %q — change it after first login\n", password)
	} else {
		fmt.Fprintln(os.Stderr, "auth: bootstrap password taken from GOSSIPPER_BOOTSTRAP_PASSWORD")
	}
}
