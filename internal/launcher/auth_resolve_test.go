package launcher

import (
	"testing"

	"github.com/sipcapture/gossipper/internal/cli"
)

func TestResolveSettingsAuthDefaultWithUIDataDir(t *testing.T) {
	dir := t.TempDir()
	cfg := cli.Config{
		UIDataDir: dir,
	}
	auth, err := ResolveSettingsAuth(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if auth == nil || !auth.Enabled() {
		t.Fatal("expected auth enabled by default when ui_data_dir is set")
	}
	_ = auth.Close()
}

func TestResolveSettingsAuthExplicitNone(t *testing.T) {
	cfg := cli.Config{
		UIDataDir: t.TempDir(),
		Auth:      cli.AuthConfig{Type: "none"},
	}
	auth, err := ResolveSettingsAuth(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if auth != nil {
		t.Fatal("expected nil auth when auth.type is none")
	}
}
