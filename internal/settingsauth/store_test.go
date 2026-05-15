package settingsauth

import (
	"context"
	"net/http"
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
