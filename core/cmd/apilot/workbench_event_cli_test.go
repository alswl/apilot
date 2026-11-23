package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/server"
)

func TestWorkbenchAdditionalCLICommands(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APILOT_DATA_DIR", dir)
	runtime, err := server.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	call := func(want int, input string, args ...string) json.RawMessage {
		t.Helper()
		rootCmd.SetIn(strings.NewReader(input))
		t.Cleanup(func() { rootCmd.SetIn(nil) })
		var out, errs bytes.Buffer
		code := execute(append(args, "--json"), &out, &errs)
		if code != want {
			t.Fatalf("%v: exit=%d want=%d stdout=%s stderr=%s", args, code, want, &out, &errs)
		}
		var response struct {
			Data  json.RawMessage
			Error *common.Error
		}
		if err := json.Unmarshal(out.Bytes(), &response); err != nil || (response.Error != nil) != (want != 0) {
			t.Fatalf("bad response: %s (%v)", &out, err)
		}
		return response.Data
	}
	call(0, "", "workbench", "health")
	call(0, "", "workbench", "ready")
	call(2, "", "workbench", "ready", "unexpected")
	var initial common.WorkbenchEvent
	if err := json.Unmarshal(call(0, "", "workbench", "watch", "--once"), &initial); err != nil || initial.Type != "snapshot" || initial.Instance == "" {
		t.Fatalf("initial event: %+v (%v)", initial, err)
	}
	call(0, "", "automation", "snapshot")
	call(4, "", "workbench", "github", "authorization", "show", "missing")
	call(4, "", "workbench", "github", "authorization", "cancel", "missing")
	call(1, "", "workbench", "github", "repositories")
	connections, err := runtime.Service.WorkbenchConnections()
	if err != nil || len(connections) != 1 {
		t.Fatalf("connections: %+v %v", connections, err)
	}
	conn := connections[0]
	body, err := json.Marshal(common.WorkbenchTaskWrite{RequestID: "cli-extra", ConnectionID: conn.ID, ConnectionVersion: conn.Version, Title: "CLI task"})
	if err != nil {
		t.Fatal(err)
	}
	var receipt common.WorkbenchTaskReceipt
	if err := json.Unmarshal(call(0, string(body), "workbench", "task", "operation", "submit", "-"), &receipt); err != nil || receipt.SourceObjectID == "" {
		t.Fatalf("create: %+v %v", receipt, err)
	}
	id := receipt.SourceObjectID
	input, _ := json.Marshal(map[string]any{"id": id, "names": []string{"verified"}})
	call(0, string(input), "workbench", "label", "set", "-")
	var labels struct{ Items []struct{ ID string } }
	if err := json.Unmarshal(call(0, "", "workbench", "label", "list"), &labels); err != nil || len(labels.Items) != 1 {
		t.Fatalf("labels: %+v %v", labels, err)
	}
	var page common.WorkbenchPage
	if err := json.Unmarshal(call(0, "", "workbench", "task", "list", "--label-id", labels.Items[0].ID), &page); err != nil || len(page.Items) != 1 || page.Items[0].ID != id {
		t.Fatalf("filtered tasks: %+v %v", page, err)
	}
	input, _ = json.Marshal(map[string]string{"id": id, "annotation_id": "cli", "body": "annotation from stdin"})
	call(0, string(input), "workbench", "task", "annotation", "put", "-")
	if got := call(0, "", "workbench", "task", "annotation", "list", id); !bytes.Contains(got, []byte("annotation from stdin")) {
		t.Fatalf("missing annotation: %s", got)
	}
	call(0, "", "workbench", "task", "annotation", "delete", id, "cli")
	if got := call(0, "", "workbench", "task", "annotation", "list", id); bytes.Contains(got, []byte("annotation from stdin")) {
		t.Fatalf("annotation not deleted: %s", got)
	}
	call(2, "invalid-json", "workbench", "task", "annotation", "put", "-")
	call(4, "", "workbench", "task", "annotation", "list", "missing")
	bad, _ := json.Marshal(map[string]any{"id": "missing", "names": []string{"verified"}})
	call(4, string(bad), "workbench", "label", "set", "-")
}
