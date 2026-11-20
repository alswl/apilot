package services

import (
	"context"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type workbenchBridgeStore interface {
	WorkbenchBridge(string) (common.WorkbenchBridgeInput, error)
	SaveWorkbenchBridge(common.WorkbenchConnection, bool, *common.WorkbenchBridgeInput) error
}

func (s *Service) prepareWorkbenchBridge(c *common.WorkbenchConnection, old common.WorkbenchConnection, input common.WorkbenchBridgeInput) (*common.WorkbenchBridgeInput, error) {
	update := input.Endpoint != "" || input.Token != "" || input.Certificate != ""
	c.WriteEnabled, c.BridgeConfigured = old.WriteEnabled, old.BridgeConfigured
	if c.Kind != common.WorkbenchObsidianTasks {
		if update || input.WriteEnabled != nil {
			return nil, common.Err("INVALID_ARGUMENT", "Bridge settings require an Obsidian connection")
		}
		return nil, nil
	}
	targetChanged := c.Vault != old.Vault || c.Scope != old.Scope || c.ReadMethod != old.ReadMethod
	if targetChanged || update {
		c.WriteEnabled = false
	}
	if input.WriteEnabled != nil {
		c.WriteEnabled = *input.WriteEnabled
	}
	if c.ReadMethod == common.WorkbenchObsidianCLI && update {
		return nil, common.Err("INVALID_ARGUMENT", "Obsidian CLI does not use REST bridge settings")
	}
	if c.ReadMethod == common.WorkbenchObsidianCLI || (!update && !c.BridgeConfigured) {
		if c.WriteEnabled && (!old.WriteEnabled || targetChanged) {
			ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
			defer cancel()
			return nil, integrations.VerifyWorkbenchObsidianCLIWrite(ctx, *c)
		}
		return nil, nil
	}

	if !update && !c.WriteEnabled {
		return nil, nil
	}
	if !update && old.WriteEnabled && !targetChanged {
		return nil, nil
	}
	store, ok := s.store.(workbenchBridgeStore)
	if !ok {
		return nil, common.Err("INTERNAL", "Obsidian bridge settings unavailable")
	}
	bridge, err := store.WorkbenchBridge(c.ID)
	if err != nil {
		return nil, err
	}
	if input.Endpoint != "" {
		bridge.Endpoint = input.Endpoint
	}
	if input.Token != "" {
		bridge.Token = input.Token
	}
	if input.Certificate != "" {
		bridge.Certificate = input.Certificate
	}
	client, err := integrations.OpenWorkbenchObsidianBridge(bridge.Endpoint, bridge.Token, bridge.Certificate)
	if err != nil {
		return nil, err
	}
	if c.WriteEnabled {
		ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
		defer cancel()
		if err := client.VerifyVault(ctx, *c); err != nil {
			return nil, err
		}
	}
	c.BridgeConfigured = true
	if update {
		return &bridge, nil
	}
	return nil, nil
}
