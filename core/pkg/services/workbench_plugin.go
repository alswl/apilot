package services

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

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

type managedConnectorRelease struct {
	process     *integrations.WorkbenchConnectorProcess
	release     common.WorkbenchConnectorRelease
	description protocol.Description
	trusted     bool
	publisher   []byte
}

func (s *Service) registeredConnector(id string) *managedConnectorRelease {
	value, _ := s.managedConnectors.Load(id)
	connector, _ := value.(*managedConnectorRelease)
	return connector
}

func (s *Service) registeredConnectors() []*managedConnectorRelease {
	var connectors []*managedConnectorRelease
	s.managedConnectors.Range(func(_, value any) bool {
		connectors = append(connectors, value.(*managedConnectorRelease))
		return true
	})
	slices.SortFunc(connectors, func(a, b *managedConnectorRelease) int {
		return strings.Compare(a.release.ConnectorID, b.release.ConnectorID)
	})
	return connectors
}

type workbenchPluginStore interface {
	WorkbenchPluginEnabled(string) (bool, error)
	SetWorkbenchPluginEnabled(string, bool) error
}

type WorkbenchPlugin struct {
	ID                    string                            `json:"id"`
	Enabled               bool                              `json:"enabled"`
	Connector             string                            `json:"connector"`
	Release               *common.WorkbenchConnectorRelease `json:"release,omitempty"`
	EndpointConfig        *protocol.ConfigSchema            `json:"endpoint_config,omitempty"`
	ScopeConfig           *protocol.ConfigSchema            `json:"scope_config,omitempty"`
	EndpointSchemaVersion int                               `json:"endpoint_schema_version,omitempty"`
	Writable              bool                              `json:"writable,omitempty"`
	Imported              bool                              `json:"imported,omitempty"`
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
	if _, ok := s.workbenchPlugins[kind]; !ok && s.registeredConnector(kind) == nil {
		return false, common.Err("INVALID_ARGUMENT", "Unknown connector type")
	}
	return s.workbenchPluginPreference(kind)
}

func (s *Service) workbenchPluginPreference(kind string) (bool, error) {
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
	for _, connector := range s.registeredConnectors() {
		id := connector.release.ConnectorID
		if id == "github-issues" {
			continue
		}
		enabled, err := s.workbenchPluginEnabled(id)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, s.workbenchPluginInfo(id, enabled))
	}
	return plugins, nil
}

func (s *Service) SetWorkbenchPluginEnabled(id string, enabled bool) (WorkbenchPlugin, error) {
	s.workbenchPluginMu.Lock()
	defer s.workbenchPluginMu.Unlock()
	if s.ctx.Err() != nil {
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
	if previous == enabled {
		return s.workbenchPluginInfo(id, enabled), nil
	}
	connectorID := id
	if id == common.WorkbenchGitHubRepository {
		connectorID = "github-issues"
	}
	connector := s.registeredConnector(connectorID)
	if enabled && connector != nil {
		if connector.trusted {
			if err := s.verifyInstalledConnector(connectorID, connector.release.PackageDigest, connector.publisher); err != nil {
				return WorkbenchPlugin{}, err
			}
		}
		fresh := *connector
		fresh.process = connector.process.Fresh()
		fresh.description, err = fresh.process.Describe(s.ctx)
		if err != nil {
			fresh.process.Close()
			return WorkbenchPlugin{}, publicConnectorError(err)
		}
		connector = &fresh
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		if enabled && connector != nil {
			connector.process.Close()
		}
		return WorkbenchPlugin{}, common.Err("CONFLICT", "App is stopping")
	}
	if err := store.SetWorkbenchPluginEnabled(id, enabled); err != nil {
		if enabled && connector != nil {
			connector.process.Close()
		}
		return WorkbenchPlugin{}, err
	}
	if connector != nil {
		if enabled {
			s.managedConnectors.Store(connectorID, connector)
		} else {
			s.cancelConnectorAuthorizations(connectorID)
			connector.process.Stop()
		}
	}
	if !enabled {
		s.cancelConnectorSyncs(id)
	}
	s.volatileRevision.Add(1)
	return s.workbenchPluginInfo(id, enabled), nil
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
	for _, connector := range s.registeredConnectors() {
		id := connector.release.ConnectorID
		if id == "github-issues" {
			continue
		}
		enabled, err := s.workbenchPluginEnabled(id)
		if err != nil {
			return nil, err
		}
		result = append(result, WorkbenchConnector{Kind: id, Name: id, PluginID: id, UserConfigurable: enabled && connector.trusted, Release: s.workbenchPluginInfo(id, enabled).Release, Capabilities: common.WorkbenchCapabilities{Read: enabled, Refresh: enabled}})
	}
	return result, nil
}

func (s *Service) workbenchConnectionCapabilities(c common.WorkbenchSnapshot) common.WorkbenchCapabilities {
	if c.PluginDisabled || c.Paused {
		return common.WorkbenchCapabilities{}
	}
	if c.ParentConnectionID != "" {
		writable := c.WriteEnabled && c.AccessState == "available" && s.registeredConnector(c.Kind).supportsWrite()
		return common.WorkbenchCapabilities{Read: true, Refresh: true, Create: writable && c.Kind == "github-issues", SetState: writable}
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
	if id == common.WorkbenchGitHubRepository {
		id = "github-issues"
	}
	if connector := s.registeredConnector(id); connector != nil {
		info.Imported = connector.trusted
		if !enabled {
			return info
		}
		release := connector.release
		info.Release = &release
		scope := connector.description.ScopeConfig
		info.ScopeConfig = &scope
		endpoint := connector.description.EndpointConfig
		info.EndpointConfig = &endpoint
		info.EndpointSchemaVersion = connector.description.EndpointConfig.Version
		info.Writable = connector.supportsWrite()
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
	s.restoreImportedConnectors()
	development := false
	if file := os.Getenv("APILOT_DEV_CONNECTOR_MANIFEST"); len(packages) == 0 && file != "" && !insideWorkbenchApp(executable) {
		dir, err := filepath.EvalSymlinks(filepath.Dir(file))
		if err != nil || !filepath.IsAbs(file) {
			return common.Err("INVALID_ARGUMENT", "Development Connector manifest must use an absolute path")
		}
		pkg, err := integrations.LoadWorkbenchConnectorPackage(filepath.Join(dir, filepath.Base(file)), dir)
		if err != nil {
			return err
		}
		if _, reserved := s.workbenchPlugins[pkg.Manifest.ConnectorID]; reserved || pkg.Manifest.ConnectorID == "github-issues" {
			return common.Err("INVALID_ARGUMENT", "Development Connector ID is reserved")
		}
		id := pkg.Manifest.ConnectorID
		owned := s.registeredConnector(id) != nil
		if store, ok := s.store.(workbenchImportStore); ok && !owned {
			digest, err := store.WorkbenchPluginDigest(s.managedPluginID(id))
			if err != nil {
				return err
			}
			owned, err = s.hasManagedConnectorPackage(store, id, digest)
			if err != nil {
				return err
			}
			if !owned && digest == pkg.Digest && id != "fixture" {
				if err := s.migrateConnectorNamespace(id, "development-connectors"); err != nil {
					return err
				}
			}
		}
		if !owned {
			development = true
			packages = append(packages, pkg)
		}
	}
	for _, pkg := range packages {
		namespace := filepath.Join(s.dir, "connectors", pkg.Manifest.ConnectorID)
		if development && pkg.Manifest.ConnectorID != "fixture" {
			namespace = filepath.Join(s.dir, "development-connectors", pkg.Manifest.ConnectorID)
		}
		if err := os.MkdirAll(namespace, 0700); err != nil {
			return err
		}
		process := integrations.NewWorkbenchConnectorProcess(pkg, namespace)
		description, err := process.Describe(s.ctx)
		if err != nil {
			process.Close()
			log.Printf("workbench: bundled Connector describe failed: %v", err)
			continue
		}
		release = &common.WorkbenchConnectorRelease{
			ConnectorID: pkg.Manifest.ConnectorID, ReleaseVersion: pkg.Manifest.ReleaseVersion,
			ProtocolVersion: protocol.Version, PackageDigest: pkg.Digest,
		}
		s.managedConnectors.Store(release.ConnectorID, &managedConnectorRelease{process: process, release: *release, description: description})
	}

	if store, ok := s.store.(interface {
		RegisterWorkbenchPluginRelease(string, *common.WorkbenchConnectorRelease) error
	}); ok {
		ids := []string{common.WorkbenchGitHubRepository, "fixture"}
		if release != nil && !slices.Contains(ids, s.managedPluginID(release.ConnectorID)) {
			ids = append(ids, s.managedPluginID(release.ConnectorID))
		}
		for _, id := range ids {
			var current *common.WorkbenchConnectorRelease
			if release != nil && id == s.managedPluginID(release.ConnectorID) {
				current = release
			}
			if err := store.RegisterWorkbenchPluginRelease(id, current); err != nil {
				return err
			}
		}
	}
	if store, ok := s.store.(workbenchManagedConnectorStore); ok {
		return store.RecoverWorkbenchConnectorRuns()
	}
	return nil
}

func insideWorkbenchApp(executable string) bool {
	for dir := filepath.Dir(executable); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if strings.EqualFold(filepath.Ext(dir), ".app") {
			return true
		}
	}
	return false
}

func (s *Service) closeWorkbenchConnector() {
	for _, connector := range s.registeredConnectors() {
		connector.process.Close()
	}
}

func (c *managedConnectorRelease) supportsWrite() bool {
	if c == nil {
		return false
	}
	if c.release.ConnectorID == "github-issues" || c.release.ConnectorID == "fixture" && !c.trusted {
		return true
	}
	return c.trusted && slices.Contains(c.description.Capabilities, protocol.Mutate)
}

func (s *Service) cancelConnectorAuthorizations(id string) {
	for _, session := range s.workbenchConnectorAuth {
		if session.status.ConnectorID == id {
			session.cancel()
			session.status.Status = "cancelled"
			session.status.VerificationURL, session.status.UserCode = "", ""
		}
	}
}

func (s *Service) cancelConnectorSyncs(id string) {
	connections, _ := s.WorkbenchConnections()
	for _, connection := range connections {
		if connection.Kind == id || connection.ParentConnectionID != "" && s.managedPluginID(connection.Kind) == id {
			if cancel := s.workbenchActive[connection.ID]; cancel != nil {
				cancel()
			}
		}
	}
}
