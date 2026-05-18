package settingsauth

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreUserVerify(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "settings.sqlite")
	db, err := OpenStore(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := CreateUser(ctx, db, "alice", "hunter22!!"); err != nil {
		t.Fatal(err)
	}
	id, err := VerifyUser(ctx, db, "alice", "hunter22!!")
	if err != nil || id < 1 {
		t.Fatalf("verify: id=%d err=%v", id, err)
	}
	if _, err := VerifyUser(ctx, db, "alice", "wrong-pass"); err == nil {
		t.Fatal("expected verify failure")
	}
}

func TestOpenBootstrapGeneratesSecret(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "settings.sqlite")
	a, err := OpenBootstrap(dbpath, "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if !a.Enabled() {
		t.Fatal("expected auth enabled")
	}
	a2, err := Open(dbpath, "")
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	if !a2.Enabled() {
		t.Fatal("expected persisted secret on reopen")
	}
}

func TestAuthJWT(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "settings.sqlite")
	a, err := Open(dbpath, "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx := context.Background()
	if err := a.CreateUser(ctx, "bob", "password88"); err != nil {
		t.Fatal(err)
	}
	tok, exp, err := a.Login(ctx, "bob", "password88")
	if err != nil || tok == "" || exp == 0 {
		t.Fatalf("login: %v", err)
	}
	req, err := http.NewRequest(http.MethodGet, "http://localhost/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if !a.ValidRequest(req) {
		t.Fatal("expected ValidRequest true")
	}
}

func TestRotateSecretInvalidatesOldTokens(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "settings.sqlite")
	a, err := Open(dbpath, "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx := context.Background()
	if err := a.CreateUser(ctx, "carol", "password88"); err != nil {
		t.Fatal(err)
	}
	tok, _, err := a.Login(ctx, "carol", "password88")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	if !a.ValidRequest(req) {
		t.Fatal("token should be valid before rotate")
	}
	newSecret, err := a.RotateSecret(ctx)
	if err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}
	if len(newSecret) < 32 {
		t.Fatalf("expected ≥32 hex chars, got %q", newSecret)
	}
	if a.ValidRequest(req) {
		t.Fatal("expected old token rejected after rotation")
	}
	// New login should also work with the rotated key.
	tok2, _, err := a.Login(ctx, "carol", "password88")
	if err != nil {
		t.Fatal(err)
	}
	req2, _ := http.NewRequest(http.MethodGet, "http://localhost/", nil)
	req2.Header.Set("Authorization", "Bearer "+tok2)
	if !a.ValidRequest(req2) {
		t.Fatal("new token should be valid")
	}
}

func TestRotatedSecretSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "settings.sqlite")
	a1, err := Open(dbpath, "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := a1.RotateSecret(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := a1.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen with a different (or empty) cli secret; the rotated value in
	// kv_settings wins.
	a2, err := Open(dbpath, "anothersecret123456")
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	if string(a2.signingKey()) != rotated {
		t.Fatalf("expected persisted rotated secret, got different key")
	}
}

func TestOpenStoreCreatesNestedParent(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "nested", "deep", "settings.sqlite")
	db, err := OpenStore(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := os.Stat(dbpath); err != nil {
		t.Fatalf("sqlite file: %v", err)
	}
}
