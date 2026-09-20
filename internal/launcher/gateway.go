package launcher

import (
	"context"
	"strings"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/engine"
	"github.com/sipcapture/gossipper/internal/gateway"
)

// AttachGateway constructs the SIP REGISTER controller, seeds persist, wires the
// UAS engine, and starts the refresh loop when register=true.
func AttachGateway(parent context.Context, cfg *cli.Config, eng *engine.Engine) *gateway.Controller {
	port := gatewayContactPort(cfg)
	seed := seedGatewayProfiles(cfg, port)
	dataDir := strings.TrimSpace(cfg.UIDataDir)
	if dataDir != "" {
		_ = gateway.SeedPersistIfMissing(dataDir, seed)
		if loaded, ok, err := gateway.LoadPersist(dataDir); err == nil && ok {
			for i := range loaded {
				if port > 0 {
					loaded[i].ContactPort = port
				}
			}
			seed = loaded
		}
	}
	ctl := gateway.NewControllerProfiles(parent, seed, dataDir)
	ctl.SetDefaultContactPort(port)
	if eng != nil {
		ctl.AttachEngine(eng)
	}
	ctl.Start()
	cfg.Gateway = ctl.Config()
	cfg.Gateways = nil
	for _, snap := range ctl.List() {
		cfg.Gateways = append(cfg.Gateways, snap.Config)
	}
	return ctl
}

func seedGatewayProfiles(cfg *cli.Config, port int) []gateway.Config {
	var seed []gateway.Config
	g := cfg.Gateway
	if g.Domain != "" || g.Addr != "" || g.Username != "" || g.ID != "" {
		g.Enabled = true
		if g.ContactPort <= 0 {
			g.ContactPort = port
		}
		gateway.EnsureIdentity(&g)
		seed = append(seed, g)
	}
	for i := range cfg.Gateways {
		item := cfg.Gateways[i]
		if item.ContactPort <= 0 {
			item.ContactPort = port
		}
		gateway.EnsureIdentity(&item)
		seed = append(seed, item)
	}
	return seed
}

func gatewayContactPort(cfg *cli.Config) int {
	if cfg == nil {
		return 0
	}
	if len(cfg.ServerListeners) > 0 && cfg.ServerListeners[0].LocalPort > 0 {
		return cfg.ServerListeners[0].LocalPort
	}
	if cfg.LocalPort > 0 {
		return cfg.LocalPort
	}
	if cfg.Gateway.ContactPort > 0 {
		return cfg.Gateway.ContactPort
	}
	return 0
}
