package services

import (
	"bytes"
	"context"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
	"golang.org/x/mod/semver"
)

type workbenchImportStore interface {
	WorkbenchPluginDigest(string) (string, error)
	RegisterWorkbenchPluginRelease(string, *common.WorkbenchConnectorRelease) error
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
	namespace := filepath.Join(s.dir, "connectors", pkg.Manifest.ConnectorID)
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
	if prior == nil && digest != "" || prior != nil && (!prior.trusted || !bytes.Equal(prior.publisher, key) || semver.Compare("v"+pkg.Manifest.ReleaseVersion, "v"+prior.release.ReleaseVersion) <= 0) {
		return empty, common.Err("CONFLICT", "Connector ID, publisher or release conflicts with the installed Connector")
	}
	enabled, err := s.workbenchPluginPreference(id)
	if err != nil {
		return empty, err
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
	stop()
	cancel()
	process.Close()
	if err != nil {
		return empty, common.Err("INVALID_ARGUMENT", "Connector identity handshake failed")
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
	if err := store.RegisterWorkbenchPluginRelease(id, &connector.release); err != nil {
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
