package services

import (
	"bytes"
	"context"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
	"golang.org/x/mod/semver"
)

type workbenchImportStore interface {
	WorkbenchPluginDigest(string) (string, error)
	WorkbenchManagedPlugin(string) (bool, error)
	RegisterWorkbenchManagedRelease(string, common.WorkbenchConnectorRelease) error
}

func (s *Service) connectorPackageRoot() (string, error) {
	dir, err := filepath.EvalSymlinks(s.dir)
	return filepath.Join(dir, "connector-packages"), err
}

func (s *Service) verifyImportedConnector(dir string) (integrations.WorkbenchConnectorPackage, []byte, error) {
	dataDir, err := filepath.EvalSymlinks(s.dir)
	if err != nil {
		return integrations.WorkbenchConnectorPackage{}, nil, err
	}
	pkg, key, err := integrations.VerifySignedWorkbenchConnector(dir, filepath.Join(dataDir, "connector-publishers.json"))
	if err != nil {
		return pkg, nil, common.Err("INVALID_ARGUMENT", err.Error())
	}
	id := pkg.Manifest.ConnectorID
	if _, reserved := s.workbenchPlugins[id]; reserved || id == "github-issues" || id == "fixture" {
		return pkg, nil, common.Err("INVALID_ARGUMENT", "Connector ID is reserved")
	}
	if !semver.IsValid("v" + pkg.Manifest.ReleaseVersion) {
		return pkg, nil, common.Err("INVALID_ARGUMENT", "Connector version is invalid")
	}
	return pkg, key, nil
}

func (s *Service) newImportedConnector(pkg integrations.WorkbenchConnectorPackage, key []byte, description protocol.Description) (*managedConnectorRelease, error) {
	namespace := filepath.Join(s.dir, "managed-connectors", pkg.Manifest.ConnectorID)
	if err := os.MkdirAll(namespace, 0700); err != nil {
		return nil, err
	}
	return &managedConnectorRelease{
		process:     integrations.NewWorkbenchConnectorProcess(pkg, namespace),
		release:     common.WorkbenchConnectorRelease{ConnectorID: pkg.Manifest.ConnectorID, ReleaseVersion: pkg.Manifest.ReleaseVersion, ProtocolVersion: protocol.Version, PackageDigest: pkg.Digest},
		description: description, trusted: true, publisher: key,
	}, nil
}

func (s *Service) ImportWorkbenchConnector(ctx context.Context, path string) (WorkbenchPlugin, error) {
	var empty WorkbenchPlugin
	if !filepath.IsAbs(path) {
		return empty, common.Err("INVALID_ARGUMENT", "Connector package path must be absolute")
	}
	store, ok := s.store.(workbenchImportStore)
	if !ok {
		return empty, common.Err("INTERNAL", "Connector installation storage unavailable")
	}
	root, err := s.connectorPackageRoot()
	if err != nil {
		return empty, err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return empty, err
	}
	stage, err := integrations.StageWorkbenchConnector(path, root)
	if err != nil {
		return empty, common.Err("INVALID_ARGUMENT", err.Error())
	}
	defer os.RemoveAll(stage)
	pkg, key, err := s.verifyImportedConnector(stage)
	if err != nil {
		return empty, err
	}
	id := pkg.Manifest.ConnectorID
	s.workbenchPluginMu.Lock()
	defer s.workbenchPluginMu.Unlock()
	if s.ctx.Err() != nil {
		return empty, common.Err("CONFLICT", "App is stopping")
	}
	prior := s.registeredConnector(id)
	digest, err := store.WorkbenchPluginDigest(id)
	if err != nil {
		return empty, err
	}
	owned, err := s.hasManagedConnectorPackage(store, id, digest)
	if err != nil {
		return empty, err
	}
	if owned && (prior == nil || !prior.trusted) || prior != nil && prior.trusted && (!bytes.Equal(prior.publisher, key) || semver.Compare("v"+pkg.Manifest.ReleaseVersion, "v"+prior.release.ReleaseVersion) <= 0) {
		return empty, common.Err("CONFLICT", "Connector ID, publisher or release conflicts with the installed Connector")
	}
	enabled, err := s.workbenchPluginPreference(id)
	if err != nil {
		return empty, err
	}
	saved, err := s.savedConnectorContexts(id)
	if err != nil {
		return empty, err
	}
	if (prior == nil || !prior.trusted) && len(saved) != 0 {
		return empty, common.Err("CONFLICT", "Existing development connections must be removed before importing this ID")
	}
	// Run the identity handshake with an isolated namespace before granting access
	// to the installed Connector's account state.
	handshakeDir, err := os.MkdirTemp(stage, ".handshake-")
	if err != nil {
		return empty, err
	}
	process := integrations.NewWorkbenchConnectorProcess(pkg, handshakeDir)
	handshakeContext, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	description, err := process.Describe(handshakeContext)
	if err == nil {
		err = validateSavedConnectorContexts(handshakeContext, process, description, saved)
	} else {
		err = common.Err("INVALID_ARGUMENT", "Connector identity handshake failed")
	}
	stop()
	cancel()
	process.Close()
	if err != nil {
		return empty, err
	}
	if err := os.RemoveAll(handshakeDir); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	dir := filepath.Join(root, id, pkg.Digest)
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		return empty, err
	}
	// A process can stop after moving a candidate but before recording its
	// digest. Only the database-selected directory is active.
	if digest != pkg.Digest {
		if err := os.RemoveAll(dir); err != nil {
			return empty, err
		}
	}
	if err := os.Rename(stage, dir); err != nil {
		return empty, err
	}
	activated := false
	defer func() {
		if !activated {
			os.RemoveAll(dir)
		}
	}()
	pkg, key, err = s.verifyImportedConnector(dir)
	if err != nil {
		return empty, err
	}
	connector, err := s.newImportedConnector(pkg, key, description)
	if err != nil {
		return empty, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping || ctx.Err() != nil {
		connector.process.Close()
		return empty, common.Err("CONFLICT", "Connector import was interrupted")
	}
	current, err := s.savedConnectorContexts(id)
	if err != nil || !reflect.DeepEqual(current, saved) {
		connector.process.Close()
		if err != nil {
			return empty, err
		}
		return empty, common.Err("STALE_REVISION", "Connector configuration changed during package validation")
	}
	if err := store.RegisterWorkbenchManagedRelease(id, connector.release); err != nil {
		connector.process.Close()
		return empty, err
	}
	activated = true
	if prior != nil {
		s.cancelConnectorAuthorizations(id)
		prior.process.Stop()
		s.cancelConnectorSyncs(id)
	}
	s.managedConnectors.Store(id, connector)
	s.volatileRevision.Add(1)
	return s.workbenchPluginInfo(id, enabled), nil
}

func (s *Service) restoreImportedConnectors() {
	store, ok := s.store.(workbenchImportStore)
	if !ok {
		return
	}
	root, err := s.connectorPackageRoot()
	if err != nil {
		log.Printf("workbench: cannot locate Connector installation directory: %v", err)
		return
	}
	dirs, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		log.Printf("workbench: cannot open Connector installation directory: %v", err)
		return
	}
	for _, dir := range dirs {
		if !dir.IsDir() || dir.Name()[0] == '.' {
			continue
		}
		if err := s.restoreImportedConnector(store, root, dir.Name()); err != nil {
			log.Printf("workbench: installed Connector %q unavailable: %v", dir.Name(), err)
		}
	}
}

func (s *Service) restoreImportedConnector(store workbenchImportStore, root, id string) error {
	digest, err := store.WorkbenchPluginDigest(id)
	if err != nil || digest == "" {
		return err
	}
	if b, err := hex.DecodeString(digest); err != nil || len(b) != 32 {
		return common.Err("INVALID_ARGUMENT", "Invalid installed Connector digest")
	}
	pkg, key, err := s.verifyImportedConnector(filepath.Join(root, id, digest))
	if err != nil {
		return err
	}
	if pkg.Manifest.ConnectorID != id || pkg.Digest != digest || s.registeredConnector(id) != nil {
		return common.Err("CONFLICT", "Installed Connector identity conflicts")
	}
	enabled, err := s.workbenchPluginPreference(id)
	if err != nil {
		return err
	}
	managed, err := store.WorkbenchManagedPlugin(id)
	if err != nil {
		return err
	}
	if !managed {
		if err := s.migrateConnectorNamespace(id, "managed-connectors"); err != nil {
			return err
		}
	}
	connector, err := s.newImportedConnector(pkg, key, protocol.Description{})
	if err != nil {
		return err
	}
	if enabled {
		connector.description, err = connector.process.Describe(s.ctx)
		if err != nil {
			connector.process.Close()
			return err
		}
	}
	if err := store.RegisterWorkbenchManagedRelease(id, connector.release); err != nil {
		connector.process.Close()
		return err
	}
	s.managedConnectors.Store(id, connector)
	return nil
}

func (s *Service) verifyInstalledConnector(id, digest string, publisher []byte) error {
	root, err := s.connectorPackageRoot()
	if err != nil {
		return err
	}
	pkg, key, err := s.verifyImportedConnector(filepath.Join(root, id, digest))
	if err != nil {
		return err
	}
	if pkg.Manifest.ConnectorID != id || pkg.Digest != digest || !bytes.Equal(key, publisher) {
		return common.Err("CONFLICT", "Installed Connector identity changed")
	}
	return nil
}

func (s *Service) hasManagedConnectorPackage(store workbenchImportStore, id, digest string) (bool, error) {
	managed, err := store.WorkbenchManagedPlugin(id)
	if err != nil || managed {
		return managed, err
	}
	// Releases installed before the ownership column are identified by their
	// database-selected directory and promoted during successful restoration.
	if b, err := hex.DecodeString(digest); err != nil || len(b) != 32 {
		return false, nil
	}
	root, err := s.connectorPackageRoot()
	if err != nil {
		return false, err
	}
	// Reserve a managed slot even if its signature or files cannot be loaded.
	_, err = os.Lstat(filepath.Join(root, id, digest))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *Service) savedConnectorContexts(id string) (map[string]protocol.ScopeContext, error) {
	store, err := s.managedConnectorStore()
	if err != nil {
		return nil, err
	}
	overviews, err := store.WorkbenchConnectorOverviews("")
	if err != nil {
		return nil, err
	}
	saved := make(map[string]protocol.ScopeContext)
	for _, account := range overviews {
		if account.Connection.ConnectorID != id {
			continue
		}
		for _, entry := range account.Scopes {
			scope, err := store.WorkbenchConnectorScope(entry.ScopeID)
			if err != nil {
				return nil, err
			}
			saved[entry.ScopeID] = scope.Context
		}
	}
	return saved, nil
}

func validateSavedConnectorContexts(ctx context.Context, process *integrations.WorkbenchConnectorProcess, description protocol.Description, saved map[string]protocol.ScopeContext) error {
	for _, scope := range saved {
		if scope.ScopeType != "" && !slices.Contains(description.ScopeConfig.Types, scope.ScopeType) {
			return common.Err("CONFLICT", "New Connector release does not support a saved scope type")
		}
		expected := scope.ScopeRef
		scope.ScopeRef = ""
		result, err := callWorkbenchConnector[protocol.ValidatedScope](ctx, process, common.ID(), protocol.Validate, protocol.ValidateParams{ScopeContext: scope, Mode: "static"}, nil)
		if err != nil || result.ScopeRef != expected || result.EndpointRef != scope.EndpointRef || result.Label == "" {
			return common.Err("CONFLICT", "New Connector release cannot validate a saved configuration and its original scope reference")
		}
	}
	return nil
}

func (s *Service) migrateConnectorNamespace(id, kind string) error {
	old := filepath.Join(s.dir, "connectors", id)
	target := filepath.Join(s.dir, kind, id)
	if info, err := os.Lstat(target); !os.IsNotExist(err) {
		if err == nil && !info.IsDir() {
			return common.Err("CONFLICT", "Connector namespace is not a directory")
		}
		return err
	}
	info, err := os.Lstat(old)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return common.Err("CONFLICT", "Legacy Connector namespace is not a directory")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	return os.Rename(old, target)
}
