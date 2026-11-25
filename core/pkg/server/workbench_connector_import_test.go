package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
)

func TestSignedScopedConnector(t *testing.T) {
	t.Setenv("APILOT_DEV_CONNECTOR_MANIFEST", "")
	packages := t.TempDir()
	const id = "synthetic-scopes"
	build := func(id, version, scopeMode string) string {
		t.Helper()
		cmd := exec.Command("bash", "../../../scripts/build-scoped-connector-fixture.sh", packages, version, id, scopeMode)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build synthetic package: %v\n%s", err, out)
		}
		return filepath.Join(packages, id+"-"+version+".apilotconnector")
	}
	initial, upgrade := build(id, "0.1.0", "both"), build(id, "0.2.0", "both")
	const secondID = "synthetic-project"
	second := build(secondID, "0.1.0", "project")
	rawTrust, err := os.ReadFile(filepath.Join(packages, "connector-publishers.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var s *services.Service
	var api uc01TestAPI
	open := func() {
		var err error
		s, err = OpenService(dir)
		if err != nil {
			t.Fatal(err)
		}
		router, _ := workbenchRouter(s)
		api = uc01TestAPI{t: t, handler: router}
	}
	open()
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	expect := func(method, path string, body any, status int) {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, workbenchPath+path, bytes.NewReader(raw))
		req.Header.Set(common.WorkbenchAPIVersionHeader, common.WorkbenchAPIVersion)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		api.handler.ServeHTTP(response, req)
		if response.Code != status {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, response.Code, status, response.Body.String())
		}
	}
	sourceDir := filepath.Join(dir, "connectors", id)
	expect(http.MethodPost, "/plugins/import", map[string]string{"path": initial}, http.StatusBadRequest)
	if _, err := os.Stat(sourceDir); !os.IsNotExist(err) {
		t.Fatalf("untrusted package changed source namespace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "connector-publishers.json"), rawTrust, 0600); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(packages, "fixture-publisher.key"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"digest", "signature", "architecture", "protocol", "identity", "duplicate", "traversal", "link"} {
		t.Run(name, func(t *testing.T) {
			path := corruptConnectorFixture(t, initial, name, ed25519.PrivateKey(key))
			expect(http.MethodPost, "/plugins/import", map[string]string{"path": path}, http.StatusBadRequest)
			if _, err := os.Stat(sourceDir); !os.IsNotExist(err) {
				t.Fatalf("rejected package changed source namespace: %v", err)
			}
		})
	}
	var plugin services.WorkbenchPlugin
	api.call(http.MethodPost, "/plugins/import", map[string]string{"path": initial}, &plugin)
	if !plugin.Enabled || !plugin.Imported || !plugin.Writable || len(plugin.ScopeConfig.Types) != 2 {
		t.Fatalf("installed Connector: %+v", plugin)
	}
	if entries, err := os.ReadDir(sourceDir); err != nil || len(entries) != 0 {
		t.Fatalf("import performed source work: %v %v", entries, err)
	}
	var single services.WorkbenchPlugin
	api.call(http.MethodPost, "/plugins/import", map[string]string{"path": second}, &single)
	if single.ScopeConfig == nil || len(single.ScopeConfig.Types) != 1 || single.ScopeConfig.Types[0] != protocol.ScopeProject {
		t.Fatalf("single-scope Connector: %+v", single)
	}
	var auth common.WorkbenchConnectorAuthorization
	api.call(http.MethodPost, "/connector-authorizations", common.WorkbenchConnectorLoginInput{ConnectorID: id}, &auth)
	login := api.status(auth.ID, func(a common.WorkbenchConnectorAuthorization) bool { return a.Status != "waiting" })
	if login.Status != "authorized" {
		t.Fatalf("login: %+v", login)
	}
	optionsPath := "/connectors/" + id + "/options?field=target&authorization_id=" + auth.ID
	var options common.WorkbenchConnectorOptions
	api.call(http.MethodGet, optionsPath+"&scope_type=project", nil, &options)
	if len(options.Items) != 1 || options.Items[0].Value != "alpha" || options.NextPage == "" {
		t.Fatalf("first page: %+v", options)
	}
	token := url.QueryEscape(options.NextPage)
	expect(http.MethodGet, optionsPath+"&scope_type=space&page="+token, nil, http.StatusBadRequest)
	api.call(http.MethodGet, optionsPath+"&scope_type=project&page="+token, nil, &options)
	if len(options.Items) != 1 || options.Items[0].Value != "beta" {
		t.Fatalf("second page: %+v", options)
	}
	api.call(http.MethodGet, optionsPath+"&scope_type=project&search=be", nil, &options)
	if len(options.Items) != 1 || options.Items[0].Value != "beta" {
		t.Fatalf("search result: %+v", options)
	}
	save := func(kind, target string) common.WorkbenchConnectorSaveResult {
		t.Helper()
		var output common.WorkbenchConnectorSaveResult
		api.call(http.MethodPut, "/connector-connections", common.WorkbenchConnectorSaveInput{AuthorizationID: auth.ID, EndpointSchemaVersion: 1, ScopeSchemaVersion: 1,
			Scopes: []common.WorkbenchConnectorSelection{{ScopeType: kind, Config: map[string]any{"target": target}, WriteEnabled: true}}}, &output)
		if len(output.Scopes) != 1 || output.Scopes[0].Status != "queued" {
			t.Fatalf("save scope: %+v", output)
		}
		return output
	}
	alpha := save(protocol.ScopeProject, "alpha")
	alphaID := alpha.Scopes[0].ScopeID
	uc01Wait(t, func() bool { return api.scopeTasks(alphaID).Total == 1 })
	api.call(http.MethodGet, optionsPath+"&scope_type=space", nil, &options)
	if len(options.ConnectedValues) != 0 {
		t.Fatalf("a project hid the same opaque space value: %+v", options)
	}
	betaID := save(protocol.ScopeProject, "beta").Scopes[0].ScopeID
	space := save(protocol.ScopeSpace, "alpha")
	spaceID := space.Scopes[0].ScopeID
	if alphaID == spaceID || space.ConnectionID != alpha.ConnectionID {
		t.Fatalf("same-name scope collided: %+v", space)
	}
	uc01Wait(t, func() bool { return api.scopeTasks(betaID).Total == 1 && api.scopeTasks(spaceID).Total == 2 })
	alphaTask, betaTask := api.scopeTasks(alphaID).Items[0], api.scopeTasks(betaID).Items[0]
	api.call(http.MethodGet, "/tasks/"+alphaTask.ID+"/detail?scope_id="+alphaID, nil, nil)
	expect(http.MethodGet, "/tasks/"+betaTask.ID+"/detail?scope_id="+alphaID, nil, http.StatusForbidden)
	write := common.WorkbenchTaskWrite{RequestID: "scoped-cross", ConnectionID: alphaID, ConnectionVersion: alphaTask.ConnectionVersion,
		SourceObjectID: betaTask.ID, TargetVersion: betaTask.TargetVersion, Fields: map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, "Edited Alpha")}}
	expect(http.MethodPost, "/task-operations", write, http.StatusBadRequest)
	write.RequestID, write.SourceObjectID, write.TargetVersion = "scoped-write", alphaTask.ID, alphaTask.TargetVersion
	api.submit(write, "confirmed")
	if api.task(betaTask.ID).Title != betaTask.Title || api.task(alphaTask.ID).Title != "Edited Alpha" {
		t.Fatal("write did not preserve scope isolation")
	}
	api.call(http.MethodPut, "/plugins/"+id+"/enabled", map[string]bool{"enabled": false}, nil)
	expect(http.MethodPost, "/connector-authorizations", common.WorkbenchConnectorLoginInput{ConnectorID: id}, http.StatusConflict)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	open()
	expect(http.MethodPost, "/connector-authorizations", common.WorkbenchConnectorLoginInput{ConnectorID: id}, http.StatusConflict)
	var secondAuth common.WorkbenchConnectorAuthorization
	api.call(http.MethodPost, "/connector-authorizations", common.WorkbenchConnectorLoginInput{ConnectorID: secondID}, &secondAuth)
	if api.status(secondAuth.ID, func(a common.WorkbenchConnectorAuthorization) bool { return a.Status != "waiting" }).Status != "authorized" {
		t.Fatal("disabling one Connector affected another")
	}
	expect(http.MethodGet, "/connectors/"+id+"/options?field=target&scope_type=project&authorization_id="+secondAuth.ID, nil, http.StatusNotFound)
	api.call(http.MethodPut, "/plugins/"+id+"/enabled", map[string]bool{"enabled": true}, nil)
	api.call(http.MethodPost, "/plugins/import", map[string]string{"path": upgrade}, &plugin)
	if plugin.Release == nil || plugin.Release.ReleaseVersion != "0.2.0" {
		t.Fatalf("upgrade: %+v", plugin)
	}
	expect(http.MethodPost, "/plugins/import", map[string]string{"path": initial}, http.StatusConflict)
	brokenUpgrade := corruptConnectorFixture(t, upgrade, "identity", ed25519.PrivateKey(key))
	expect(http.MethodPost, "/plugins/import", map[string]string{"path": brokenUpgrade}, http.StatusBadRequest)
	var overview common.WorkbenchConnectorOverview
	api.call(http.MethodGet, "/connector-connections/"+alpha.ConnectionID, nil, &overview)
	if len(overview.Scopes) != 3 || api.scopeTasks(alphaID).Total != 1 {
		t.Fatalf("upgrade lost scopes: %+v", overview)
	}
	if _, err := s.WorkbenchConnectorTaskDetail(context.Background(), alphaTask.ID, "fields", "", alphaID); err != nil {
		t.Fatal(err)
	}
}

func corruptConnectorFixture(t *testing.T, original, kind string, key ed25519.PrivateKey) string {
	t.Helper()
	pkg, err := zip.OpenReader(original)
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	data := map[string][]byte{}
	for _, entry := range pkg.File {
		opened, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(opened)
		opened.Close()
		if err != nil {
			t.Fatal(err)
		}
		data[entry.Name] = contents
	}
	var manifest protocol.Manifest
	if err := json.Unmarshal(data["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	entries := []string{"manifest.json", "manifest.sig", "connector"}
	switch kind {
	case "signature":
		data["manifest.sig"][0] ^= 0xff
	case "digest":
		data["connector"][0] ^= 0xff
	case "architecture", "protocol", "identity":
		switch kind {
		case "architecture":
			data["connector"] = []byte("not a native executable")
			manifest.EntrypointSHA256 = fmt.Sprintf("%x", sha256.Sum256(data["connector"]))
		case "protocol":
			manifest.ProtocolVersions = []int{2}
		case "identity":
			manifest.ReleaseVersion = "9.0.0"
		}
		data["manifest.json"], err = json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		data["manifest.sig"] = ed25519.Sign(key, data["manifest.json"])
	case "duplicate":
		entries[1] = entries[0]
	case "traversal":
		entries[2] = "../connector"
	}
	output := filepath.Join(t.TempDir(), kind+".apilotconnector")
	f, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, name := range entries {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if kind == "link" && name == "connector" {
			header.SetMode(os.ModeSymlink | 0777)
		}
		w, err := z.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data[strings.TrimPrefix(name, "../")]); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return output
}
