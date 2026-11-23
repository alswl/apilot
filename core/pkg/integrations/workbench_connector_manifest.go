package integrations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"

	"github.com/alswl/apilot/connectors/protocol"
)

const bundledGitHubConnector = "github-issues"

var (
	connectorID          = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	connectorVersion     = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	errConnectorManifest = errors.New("invalid Connector manifest or entrypoint")
)

type WorkbenchConnectorPackage struct {
	Manifest   protocol.Manifest
	Digest     string
	entrypoint string
}

func DiscoverBundledWorkbenchConnectors(executable string) ([]WorkbenchConnectorPackage, error) {
	macos := filepath.Dir(executable)
	contents := filepath.Dir(macos)
	if runtime.GOOS != "darwin" || filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(filepath.Dir(contents)) != ".app" {
		return nil, nil
	}
	manifests := filepath.Join(contents, "Resources", "connectors")
	if _, err := os.Lstat(manifests); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, errConnectorManifest
	}
	pkg, err := LoadWorkbenchConnectorPackage(
		filepath.Join(manifests, bundledGitHubConnector+".json"),
		filepath.Join(contents, "Helpers", "connectors"),
	)
	if err != nil {
		return nil, err
	}
	if pkg.Manifest.ConnectorID != bundledGitHubConnector || pkg.Manifest.Entrypoint != "apilot-github-issues" {
		return nil, errConnectorManifest
	}
	return []WorkbenchConnectorPackage{pkg}, nil
}

// LoadWorkbenchConnectorPackage accepts a manifest and helper directory from an
// already trusted distribution or an explicitly selected development package.
func LoadWorkbenchConnectorPackage(manifestPath, helpers string) (WorkbenchConnectorPackage, error) {
	var pkg WorkbenchConnectorPackage
	manifest, err := openConnectorFile(manifestPath, false)
	if err != nil {
		return pkg, err
	}
	defer manifest.Close()
	data, err := io.ReadAll(io.LimitReader(manifest, protocol.MaxLoginNotification+1))
	if err != nil || len(data) > protocol.MaxLoginNotification || json.Unmarshal(data, &pkg.Manifest) != nil {
		return pkg, errConnectorManifest
	}
	m := pkg.Manifest
	if m.ManifestVersion != protocol.ManifestVersion || !connectorID.MatchString(m.ConnectorID) ||
		!connectorVersion.MatchString(m.ReleaseVersion) || !slices.Contains(m.ProtocolVersions, protocol.Version) ||
		m.OS != runtime.GOOS || m.Arch != runtime.GOARCH || !filepath.IsLocal(m.Entrypoint) ||
		filepath.Clean(m.Entrypoint) != m.Entrypoint || m.Entrypoint == "." {
		return pkg, errConnectorManifest
	}
	pkg.entrypoint = filepath.Join(helpers, m.Entrypoint)
	if err := pkg.verifyEntrypoint(); err != nil {
		return WorkbenchConnectorPackage{}, err
	}
	// The v1 package consists of the manifest and its pinned executable.
	digest := sha256.Sum256(data)
	pkg.Digest = hex.EncodeToString(digest[:])
	return pkg, nil
}

func openConnectorFile(path string, executable bool) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, errConnectorManifest
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return nil, errConnectorManifest
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || (executable && info.Mode().Perm()&0111 == 0) {
		return nil, errConnectorManifest
	}
	return os.Open(path)
}

func (p WorkbenchConnectorPackage) verifyEntrypoint() error {
	file, err := openConnectorFile(p.entrypoint, true)
	if err != nil {
		return err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil || hex.EncodeToString(h.Sum(nil)) != p.Manifest.EntrypointSHA256 {
		return errConnectorManifest
	}
	return nil
}
