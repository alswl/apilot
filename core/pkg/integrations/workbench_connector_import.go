package integrations

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/alswl/apilot/connectors/protocol"
)

// StageWorkbenchConnector extracts the fixed package layout into a private
// directory. The caller owns the directory and can rename it after handshake.
func StageWorkbenchConnector(path, root string) (string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", errors.New("Cannot open Connector package")
	}
	defer archive.Close()
	if len(archive.File) != 3 {
		return "", errors.New("Invalid Connector package layout")
	}
	dir, err := os.MkdirTemp(root, ".candidate-")
	if err != nil {
		return "", err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(dir)
		}
	}()
	limits := map[string]int64{"manifest.json": protocol.MaxLoginNotification, "manifest.sig": ed25519.SignatureSize, "connector": 128 << 20}
	for _, entry := range archive.File {
		limit, ok := limits[entry.Name]
		if !ok || !entry.Mode().IsRegular() || entry.UncompressedSize64 > uint64(limit) {
			return "", errors.New("Invalid Connector package entry")
		}
		delete(limits, entry.Name)
		if err := extractConnectorEntry(dir, entry, limit); err != nil {
			return "", err
		}
	}
	success = true
	return dir, nil
}

func extractConnectorEntry(dir string, entry *zip.File, limit int64) error {
	input, err := entry.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	mode := os.FileMode(0600)
	if entry.Name == "connector" {
		mode = 0700
	}
	output, err := os.OpenFile(filepath.Join(dir, entry.Name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(output, io.LimitReader(input, limit+1))
	closeErr := output.Close()
	if n > limit {
		return errors.New("Connector package entry exceeds size limit")
	}
	return errors.Join(copyErr, closeErr)
}

// The trust file maps Connector IDs to base64-encoded Ed25519 public keys.
func VerifySignedWorkbenchConnector(dir, trustFile string) (WorkbenchConnectorPackage, []byte, error) {
	var empty WorkbenchConnectorPackage
	manifest, err := readConnectorFile(filepath.Join(dir, "manifest.json"), protocol.MaxLoginNotification)
	if err != nil {
		return empty, nil, err
	}
	var m protocol.Manifest
	if json.Unmarshal(manifest, &m) != nil || m.Entrypoint != "connector" || !connectorID.MatchString(m.ConnectorID) {
		return empty, nil, errors.New("Invalid Connector manifest")
	}
	signature, err := readConnectorFile(filepath.Join(dir, "manifest.sig"), ed25519.SignatureSize)
	if err != nil {
		return empty, nil, err
	}
	trust, err := readConnectorFile(trustFile, 1<<20)
	if err != nil {
		return empty, nil, errors.New("Connector publisher trust is unavailable")
	}
	var keys map[string][]byte
	if json.Unmarshal(trust, &keys) != nil {
		return empty, nil, errors.New("Invalid Connector publisher trust")
	}
	key := keys[m.ConnectorID]
	if len(key) != ed25519.PublicKeySize {
		return empty, nil, errors.New("Connector publisher is not trusted for this ID")
	}
	if !ed25519.Verify(key, manifest, signature) {
		return empty, nil, errors.New("Invalid Connector publisher signature")
	}
	pkg, err := LoadWorkbenchConnectorPackage(filepath.Join(dir, "manifest.json"), dir)
	if err != nil {
		return empty, nil, errors.New("Connector manifest, platform, protocol or executable digest is invalid")
	}
	if !validNativeConnector(pkg.entrypoint) {
		return empty, nil, errors.New("Connector executable architecture does not match this platform")
	}
	digest := sha256.Sum256(manifest)
	if hex.EncodeToString(digest[:]) != pkg.Digest {
		return empty, nil, errors.New("Connector manifest changed during verification")
	}
	return pkg, key, nil
}

func readConnectorFile(path string, limit int64) ([]byte, error) {
	f, err := openConnectorFile(path, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("Cannot read Connector package metadata")
	}
	return data, nil
}

func validNativeConnector(path string) bool {
	switch runtime.GOOS {
	case "darwin":
		f, err := macho.Open(path)
		if err != nil {
			return false
		}
		defer f.Close()
		return f.Type == macho.TypeExec && (runtime.GOARCH == "arm64" && f.Cpu == macho.CpuArm64 || runtime.GOARCH == "amd64" && f.Cpu == macho.CpuAmd64)
	case "linux":
		f, err := elf.Open(path)
		if err != nil {
			return false
		}
		defer f.Close()
		return (f.Type == elf.ET_EXEC || f.Type == elf.ET_DYN) && (runtime.GOARCH == "arm64" && f.Machine == elf.EM_AARCH64 || runtime.GOARCH == "amd64" && f.Machine == elf.EM_X86_64)
	case "windows":
		f, err := pe.Open(path)
		if err != nil {
			return false
		}
		defer f.Close()
		return f.FileHeader.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE != 0 && f.FileHeader.Characteristics&pe.IMAGE_FILE_DLL == 0 &&
			(runtime.GOARCH == "arm64" && f.Machine == pe.IMAGE_FILE_MACHINE_ARM64 || runtime.GOARCH == "amd64" && f.Machine == pe.IMAGE_FILE_MACHINE_AMD64)
	}
	return false
}
