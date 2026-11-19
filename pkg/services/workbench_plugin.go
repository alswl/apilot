package services

import (
	"context"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/integrations"
)

type workbenchTaskWriter func(*Service, workbenchStore, workbenchTaskWriteStore, common.WorkbenchSnapshot, common.WorkbenchTaskWrite, string, common.WorkbenchTask) (common.WorkbenchTaskReceipt, error)

type workbenchPlugin struct {
	write        workbenchTaskWriter
	read         workbenchReader
	capabilities common.WorkbenchCapabilities
}

type workbenchPluginStore interface {
	WorkbenchPluginEnabled(string) (bool, error)
	SetWorkbenchPluginEnabled(string, bool) error
}

type WorkbenchPlugin struct {
	ID        string `json:"id"`
	Enabled   bool   `json:"enabled"`
	Connector string `json:"connector"`
}

type WorkbenchPluginRegistry struct {
	plugins map[string]workbenchPlugin
}

func BuiltinWorkbenchPluginRegistry() WorkbenchPluginRegistry {
	return WorkbenchPluginRegistry{plugins: map[string]workbenchPlugin{
		common.WorkbenchLocal: {write: (*Service).writeLocalWorkbenchTask, capabilities: common.WorkbenchCapabilities{Read: true, Create: true, SetState: true, Import: true}},
		common.WorkbenchObsidianTasks: {
			read:         integrations.ReadWorkbenchObsidian,
			write:        (*Service).writeObsidianWorkbenchTask,
			capabilities: common.WorkbenchCapabilities{Read: true, Refresh: true, Create: true, SetState: true, ListTargets: true},
		},
		common.WorkbenchGitHubIssues: {
			read: func(_ context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
				return integrations.ReadWorkbenchGitHubMock(c)
			},
			capabilities: common.WorkbenchCapabilities{Read: true, Refresh: true},
		},
	}}
}

func (s *Service) readWorkbench(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
	plugin := s.workbenchPlugins[c.Kind]
	if plugin.read == nil {
		return nil, common.Err("INVALID_ARGUMENT", "Connector does not support source refresh")
	}
	return plugin.read(ctx, c)
}

func (s *Service) workbenchPluginEnabled(kind string) (bool, error) {
	if _, ok := s.workbenchPlugins[kind]; !ok {
		return false, common.Err("INVALID_ARGUMENT", "Unknown connector type")
	}
	if store, ok := s.store.(workbenchPluginStore); ok {
		return store.WorkbenchPluginEnabled(kind)
	}
	return true, nil
}

func (s *Service) workbenchPluginAvailable(kind string) error {
	enabled, err := s.workbenchPluginEnabled(kind)
	if err != nil {
		return err
	}
	if !enabled {
		return common.Err("CONFLICT", "Connector plugin is disabled")
	}
	return nil
}

func (s *Service) WorkbenchPlugins() ([]WorkbenchPlugin, error) {
	var plugins []WorkbenchPlugin
	for _, connector := range WorkbenchConnectors() {
		enabled, err := s.workbenchPluginEnabled(connector.Kind)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, WorkbenchPlugin{ID: connector.Kind, Enabled: enabled, Connector: connector.Kind})
	}
	return plugins, nil
}

func (s *Service) SetWorkbenchPluginEnabled(id string, enabled bool) (WorkbenchPlugin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return WorkbenchPlugin{}, common.Err("CONFLICT", "App is stopping")
	}
	previous, err := s.workbenchPluginEnabled(id)
	if err != nil {
		return WorkbenchPlugin{}, err
	}
	store, ok := s.store.(workbenchPluginStore)
	if !ok {
		return WorkbenchPlugin{}, common.Err("INTERNAL", "Plugin settings unavailable")
	}
	result := WorkbenchPlugin{ID: id, Enabled: enabled, Connector: id}
	if previous == enabled {
		return result, nil
	}
	connections, err := s.WorkbenchConnections()
	if err != nil {
		return WorkbenchPlugin{}, err
	}
	if err := store.SetWorkbenchPluginEnabled(id, enabled); err != nil {
		return WorkbenchPlugin{}, err
	}
	if !enabled {
		for _, connection := range connections {
			if cancel := s.workbenchActive[connection.ID]; connection.Kind == id && cancel != nil {
				cancel()
			}
		}
	}
	return result, nil
}

func (s *Service) WorkbenchConnectors() ([]WorkbenchConnector, error) {
	var result []WorkbenchConnector
	for _, descriptor := range WorkbenchConnectors() {
		plugin, ok := s.workbenchPlugins[descriptor.Kind]
		if !ok {
			continue
		}
		enabled, err := s.workbenchPluginEnabled(descriptor.Kind)
		if err != nil {
			return nil, err
		}
		descriptor.PluginID = descriptor.Kind
		descriptor.UserConfigurable = descriptor.UserConfigurable && enabled
		descriptor.Capabilities = plugin.capabilities
		result = append(result, descriptor)
	}
	return result, nil
}

func (s *Service) workbenchConnectionCapabilities(c common.WorkbenchSnapshot) common.WorkbenchCapabilities {
	if c.PluginDisabled || c.Paused {
		return common.WorkbenchCapabilities{}
	}
	caps := s.workbenchPlugins[c.Kind].capabilities
	if c.Kind == common.WorkbenchObsidianTasks && (!c.WriteEnabled || !c.BridgeConfigured) {
		caps.Create, caps.SetState = false, false
	}
	return caps
}
