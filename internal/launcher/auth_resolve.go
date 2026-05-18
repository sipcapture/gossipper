package launcher

import (
	"fmt"
	"strings"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/settingsauth"
)

// ResolveSettingsAuth wires internal JWT auth for the management API when the
// admin console is enabled. Auth is on by default whenever ui_data_dir is set;
// pass auth.type: none in the management JSON to disable explicitly.
func ResolveSettingsAuth(cfg cli.Config) (*settingsauth.Auth, error) {
	if !cfg.AdminConsoleAuthEnabled() {
		return nil, nil
	}
	path := cfg.AuthSQLitePath()
	if path == "" {
		return nil, fmt.Errorf("settings auth: ui_data_dir or auth.sqlite_path is required")
	}
	secret := strings.TrimSpace(cfg.Auth.JWTSecret)
	return settingsauth.OpenBootstrap(path, secret)
}
