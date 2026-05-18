package settingsauth

import (
	"context"
	"path/filepath"
	"testing"
)

func TestEnsureBootstrapCreatesDefaultAdmin(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "settings.sqlite")
	db, err := OpenStore(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	created, err := EnsureBootstrap(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected admin bootstrap on empty db")
	}
	users, err := ListUsers(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Username != DefaultBootstrapUsername {
		t.Fatalf("users=%+v", users)
	}
	if _, err := VerifyUser(ctx, db, DefaultBootstrapUsername, DefaultBootstrapPassword); err != nil {
		t.Fatalf("verify default admin: %v", err)
	}
	bootstrapAt, err := GetSetting(ctx, db, kvBootstrapAt)
	if err != nil || bootstrapAt == "" {
		t.Fatalf("bootstrap_at=%q err=%v", bootstrapAt, err)
	}
	mode, err := GetSetting(ctx, db, kvAuthMode)
	if err != nil || mode != "internal" {
		t.Fatalf("auth_mode=%q err=%v", mode, err)
	}

	createdAgain, err := EnsureBootstrap(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if createdAgain {
		t.Fatal("expected no-op bootstrap when users already exist")
	}
}

func TestOpenBootstrapSeedsUserAndSecret(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "settings.sqlite")
	a, err := OpenBootstrap(dbpath, "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	secret, err := GetSetting(context.Background(), a.DB(), kvJWTSecret)
	if err != nil || len(secret) < 16 {
		t.Fatalf("jwt secret missing: %q err=%v", secret, err)
	}
	users, err := ListUsers(context.Background(), a.DB())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	tok, _, err := a.Login(context.Background(), DefaultBootstrapUsername, DefaultBootstrapPassword)
	if err != nil || tok == "" {
		t.Fatalf("login default admin: %v", err)
	}
}
