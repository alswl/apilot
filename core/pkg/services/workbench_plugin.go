package services

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/alswl/apilot/connectors/protocol"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
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
	ID        string                            `json:"id"`
	Enabled   bool                              `json:"enabled"`
	Connector string                            `json:"connector"`
	Release   *common.WorkbenchConnectorRelease `json:"release,omitempty"`
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
		common.WorkbenchGitHubRepository: {capabilities: common.WorkbenchCapabilities{Read: true, Refresh: true, Create: true, SetState: true}},
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
		plugins = append(plugins, s.workbenchPluginInfo(connector.Kind, enabled))
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
	result := s.workbenchPluginInfo(id, enabled)
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
		if enabled {
			descriptor.Release = s.workbenchPluginInfo(descriptor.Kind, true).Release
		}
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
	if (c.Kind == common.WorkbenchObsidianTasks || c.Kind == common.WorkbenchGitHubRepository) && !c.WriteEnabled {
		caps.Create, caps.SetState = false, false
	}
	if c.Kind == common.WorkbenchGitHubRepository && (c.AccessState != "available" || c.WriteDenied) {
		caps.Create, caps.SetState = false, false
	}
	return caps
}

func (s *Service) workbenchPluginInfo(id string, enabled bool) WorkbenchPlugin {
	info := WorkbenchPlugin{ID: id, Enabled: enabled, Connector: id}
	if enabled && id == common.WorkbenchGitHubRepository && s.workbenchConnectorRelease != nil {
		release := *s.workbenchConnectorRelease
		info.Release = &release
	}
	return info
}

func (s *Service) registerBundledWorkbenchConnector() error {
	var release *common.WorkbenchConnectorRelease
	executable, err := os.Executable()
	if err == nil {
		executable, err = filepath.EvalSymlinks(executable)
	}
	if err != nil {
		return err
	}
	packages, err := integrations.DiscoverBundledWorkbenchConnectors(executable)
	if err != nil {
		log.Printf("workbench: bundled Connector registration failed: %v", err)
	}
	for _, pkg := range packages {
		namespace := filepath.Join(s.dir, "connectors", pkg.Manifest.ConnectorID)
		if err := os.MkdirAll(namespace, 0700); err != nil {
			return err
		}
		process := integrations.NewWorkbenchConnectorProcess(pkg, namespace)
		if _, err := process.Describe(s.ctx); err != nil {
			process.Close()
			log.Printf("workbench: bundled Connector describe failed: %v", err)
			continue
		}
		s.workbenchConnectorProcess = process
		release = &common.WorkbenchConnectorRelease{
			ConnectorID: pkg.Manifest.ConnectorID, ReleaseVersion: pkg.Manifest.ReleaseVersion,
			ProtocolVersion: protocol.Version, PackageDigest: pkg.Digest,
		}
	}
	s.workbenchConnectorRelease = release
	if store, ok := s.store.(interface {
		RegisterWorkbenchPluginRelease(string, *common.WorkbenchConnectorRelease) error
	}); ok {
		return store.RegisterWorkbenchPluginRelease(common.WorkbenchGitHubRepository, release)
	}
	return nil
}

func (s *Service) closeWorkbenchConnector() {
	if s.workbenchConnectorProcess != nil {
		s.workbenchConnectorProcess.Close()
	}
}
