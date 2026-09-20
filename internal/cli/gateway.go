package cli

import (
	"encoding/json"
	"fmt"

	"github.com/sipcapture/gossipper/internal/gateway"
)

// applyGatewayFromTop copies optional top-level "gateway" / "gateways" JSON onto cfg.
func applyGatewayFromTop(cfg *Config, top map[string]json.RawMessage) error {
	if cfg == nil {
		return nil
	}
	if raw, ok := top["gateway"]; ok {
		g, err := gateway.DecodeConfig(raw)
		if err != nil {
			return fmt.Errorf("config gateway: %w", err)
		}
		cfg.Gateway = g
	}
	if raw, ok := top["gateways"]; ok {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("config gateways: %w", err)
		}
		out := make([]gateway.Config, 0, len(items))
		for i, item := range items {
			g, err := gateway.DecodeConfig(item)
			if err != nil {
				return fmt.Errorf("config gateways[%d]: %w", i, err)
			}
			out = append(out, g)
		}
		cfg.Gateways = out
	}
	return nil
}
