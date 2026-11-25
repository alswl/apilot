// apilot-fixture-package signs a synthetic binary for isolated acceptance.
package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/alswl/apilot/connectors/protocol"
)

func main() {
	var binary, directory, id, version string
	flag.StringVar(&binary, "binary", "", "Path to the compiled synthetic Connector")
	flag.StringVar(&directory, "out", "", "Private fixture output directory")
	flag.StringVar(&id, "id", "synthetic-scopes", "Synthetic Connector ID")
	flag.StringVar(&version, "version", "0.1.0", "Synthetic release version")
	flag.Parse()
	if err := build(binary, directory, id, version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build(binary, directory, id, version string) error {
	if directory == "" || filepath.Base(id) != id || filepath.Base(version) != version {
		return errors.New("output directory, fixture ID and version are required")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	executable, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	key, err := publisherKey(filepath.Join(directory, "fixture-publisher.key"))
	if err != nil {
		return err
	}
	trustPath := filepath.Join(directory, "connector-publishers.json")
	trust := map[string][]byte{}
	if data, err := os.ReadFile(trustPath); err == nil {
		if err := json.Unmarshal(data, &trust); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	trust[id] = key.Public().(ed25519.PublicKey)
	encoded, err := json.MarshalIndent(trust, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(trustPath, encoded, 0600); err != nil {
		return err
	}
	digest := sha256.Sum256(executable)
	manifest, err := json.Marshal(protocol.Manifest{ManifestVersion: protocol.ManifestVersion, ConnectorID: id, ReleaseVersion: version,
		ProtocolVersions: []int{protocol.Version}, OS: runtime.GOOS, Arch: runtime.GOARCH, Entrypoint: "connector", EntrypointSHA256: hex.EncodeToString(digest[:])})
	if err != nil {
		return err
	}
	path := filepath.Join(directory, id+"-"+version+".apilotconnector")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(file)
	for _, entry := range []struct {
		name string
		data []byte
	}{
		{"manifest.json", manifest}, {"manifest.sig", ed25519.Sign(key, manifest)}, {"connector", executable},
	} {
		writer, err := archive.Create(entry.name)
		if err == nil {
			_, err = writer.Write(entry.data)
		}
		if err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return err
		}
	}
	if err := errors.Join(archive.Close(), file.Close()); err != nil {
		_ = os.Remove(path)
		return err
	}
	fmt.Println(path)
	return nil
}

func publisherKey(path string) (ed25519.PrivateKey, error) {
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != ed25519.PrivateKeySize {
			return nil, errors.New("invalid synthetic publisher key")
		}
		return ed25519.PrivateKey(key), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, key, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := file.Write(key)
	return ed25519.PrivateKey(key), errors.Join(writeErr, file.Close())
}
