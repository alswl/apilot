package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alswl/apilot/core/pkg/config"
)

func TestWorkbenchCLIOperationRoutes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APILOT_DATA_DIR", dir)
	socket := config.HTTPSocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Dir(socket)) })
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 2)
	fixture := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		if r.URL.Path == "/v1/workbench/events" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"instance\":\"test\",\"sequence\":1,\"type\":\"snapshot\"}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/workbench/task-operations" {
			fmt.Fprint(w, `{"request_id":"item","sync_state":"conflict","conflict":{"action":"close","current":{"state":{"type":"text","value":"closed"}}}}`)
			return
		}
		fmt.Fprint(w, `{}`)
	})}
	go func() { _ = fixture.Serve(listener) }()
	defer fixture.Close()
	defer rootCmd.SetIn(nil)
	cases := []struct{ operation, method, path, command, body string }{
		{"health", "GET", "/health", "health", ``},
		{"ready", "GET", "/ready", "ready", ``},
		{"listWorkbenchConnections", "GET", "/v1/workbench/connections", "connection list", ``},
		{"saveWorkbenchConnection", "PUT", "/v1/workbench/connections", "connection save -", `{"name":"Local","kind":"local"}`},
		{"removeWorkbenchConnection", "DELETE", "/v1/workbench/connections/item", "connection remove item", ``},
		{"getWorkbenchConnection", "GET", "/v1/workbench/connections/item", "connection show item", ``},
		{"listWorkbenchHeadings", "GET", "/v1/workbench/connections/item/headings", "connection headings item note.md", ``},
		{"listWorkbenchNotes", "GET", "/v1/workbench/connections/item/notes", "connection notes item", ``},
		{"pauseWorkbenchConnection", "PUT", "/v1/workbench/connections/item/paused", "connection pause item 1", ``},
		{"getWorkbenchSync", "GET", "/v1/workbench/connections/item/sync", "sync show item", ``},
		{"runWorkbenchSync", "POST", "/v1/workbench/connections/item/sync", "sync run item", ``},
		{"cancelWorkbenchSync", "POST", "/v1/workbench/connections/item/sync/1/cancel", "sync cancel item 1", ``},
		{"listWorkbenchConnectors", "GET", "/v1/workbench/connectors", "connector list", ``},
		{"startWorkbenchConnectorAuthorization", "POST", "/v1/workbench/connector-authorizations", "connector login fixture", ``},
		{"getWorkbenchConnectorAuthorization", "GET", "/v1/workbench/connector-authorizations/item", "connector auth-status item", ``},
		{"cancelWorkbenchConnectorAuthorization", "DELETE", "/v1/workbench/connector-authorizations/item", "connector cancel-login item", ``},
		{"listWorkbenchConnectorOptions", "GET", "/v1/workbench/connectors/fixture/options", "connector options fixture --authorization item --field project", ``},
		{"listWorkbenchConnectorConnections", "GET", "/v1/workbench/connector-connections", "connector connection list", ``},
		{"saveWorkbenchConnectorConnection", "PUT", "/v1/workbench/connector-connections", "connector connection save -", `{"authorization_id":"item","endpoint_schema_version":1,"scope_schema_version":1,"scopes":[{"config":{"project":"PAY"}}]}`},
		{"getWorkbenchConnectorConnection", "GET", "/v1/workbench/connector-connections/item", "connector connection show item", ``},
		{"removeWorkbenchConnectorConnection", "DELETE", "/v1/workbench/connector-connections/item", "connector connection remove item", ``},
		{"syncWorkbenchConnectorConnection", "POST", "/v1/workbench/connector-connections/item/sync", "connector connection sync item --full", ``},
		{"getWorkbenchConnectorScopeConfiguration", "GET", "/v1/workbench/connector-scopes/item/configuration", "connector scope show item", ``},
		{"listWorkbenchConnectorScopeConfigurationOptions", "GET", "/v1/workbench/connector-scopes/item/configuration/options", "connector scope options item --revision 1 --field target", ``},
		{"previewWorkbenchConnectorScopeConfiguration", "POST", "/v1/workbench/connector-scopes/item/configuration/preview", "connector scope preview item -", `{"revision":1,"schema_version":1,"config":{"target":"beta"}}`},
		{"updateWorkbenchConnectorScopeConfiguration", "PUT", "/v1/workbench/connector-scopes/item/configuration", "connector scope save item -", `{"revision":1,"schema_version":1,"config":{"target":"beta"},"preview_token":"sample"}`},
		{"getWorkbenchTaskDetail", "GET", "/v1/workbench/tasks/item/detail", "task detail item", ``},
		{"getWorkbenchTaskEditMetadata", "GET", "/v1/workbench/tasks/item/edit-metadata", "task edit-metadata item", ``},
		{"getWorkbenchScopeEditMetadata", "GET", "/v1/workbench/connections/item/edit-metadata", "connection edit-metadata item", ``},
		{"getWorkbenchScopeFields", "GET", "/v1/workbench/connections/item/fields", "connection fields item", ``},
		{"getWorkbenchScopeMapping", "GET", "/v1/workbench/connections/item/mapping", "connection mapping show item", ``},
		{"previewWorkbenchScopeMapping", "POST", "/v1/workbench/connections/item/mapping/preview", "connection mapping preview item -", `{"revision":0}`},
		{"saveWorkbenchScopeMapping", "PUT", "/v1/workbench/connections/item/mapping", "connection mapping save item -", `{"revision":0}`},
		{"listWorkbenchSyncRuns", "GET", "/v1/workbench/sync-runs", "sync list --scope item", ``},
		{"getWorkbenchSyncRun", "GET", "/v1/workbench/sync-runs/item", "sync inspect item", ``},
		{"listWorkbenchSyncUsage", "GET", "/v1/workbench/sync-usage", "sync usage --scope item", ``},
		{"watchWorkbenchEvents", "GET", "/v1/workbench/events", "watch --once", ``},
		{"startGitHubAuthorization", "POST", "/v1/workbench/github-authorizations", "github authorization start", ``},
		{"cancelGitHubAuthorization", "DELETE", "/v1/workbench/github-authorizations/item", "github authorization cancel item", ``},
		{"getGitHubAuthorization", "GET", "/v1/workbench/github-authorizations/item", "github authorization show item", ``},
		{"listGitHubRepositories", "GET", "/v1/workbench/github-repositories", "github repositories --authorization item", ``},
		{"listWorkbenchLabels", "GET", "/v1/workbench/labels", "label list", ``},
		{"importWorkbenchConnector", "POST", "/v1/workbench/plugins/import", "plugin import item", ``},
		{"listWorkbenchPlugins", "GET", "/v1/workbench/plugins", "plugin list", ``},
		{"setWorkbenchPluginEnabled", "PUT", "/v1/workbench/plugins/item/enabled", "plugin enable item", ``},
		{"submitWorkbenchTaskOperation", "POST", "/v1/workbench/task-operations", "task operation submit -", `{"request_id":"item"}`},
		{"getWorkbenchTaskOperation", "GET", "/v1/workbench/task-operations/item", "task operation show item", ``},
		{"reconcileWorkbenchTaskOperation", "POST", "/v1/workbench/task-operations/item/reconcile", "task operation reconcile item", ``},
		{"listWorkbenchTasks", "GET", "/v1/workbench/tasks", "task list --label-id item", ``},
		{"createWorkbenchTask", "POST", "/v1/workbench/tasks", "task create -", `{"request_id":"item"}`},
		{"importWorkbenchTasks", "POST", "/v1/workbench/tasks/import", "task import -", `{"items":[]}`},
		{"getWorkbenchTask", "GET", "/v1/workbench/tasks/item", "task show item", ``},
		{"listWorkbenchAnnotations", "GET", "/v1/workbench/tasks/item/annotations", "task annotation list item", ``},
		{"deleteWorkbenchAnnotation", "DELETE", "/v1/workbench/tasks/item/annotations/note", "task annotation delete item note", ``},
		{"putWorkbenchAnnotation", "PUT", "/v1/workbench/tasks/item/annotations/note", "task annotation put -", `{"id":"item","annotation_id":"note","body":"body"}`},
		{"setWorkbenchLabels", "PUT", "/v1/workbench/tasks/item/labels", "label set -", `{"id":"item","names":["label"]}`},
		{"setWorkbenchTaskState", "POST", "/v1/workbench/tasks/item/state", "task complete -", `{"source_object_id":"item"}`},
	}
	schema, err := os.ReadFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if count := bytes.Count(schema, []byte("operationId: ")); count != len(cases) {
		t.Fatalf("OpenAPI lists %d operations; CLI suite covers %d", count, len(cases))
	}
	for _, tc := range cases {
		if !bytes.Contains(schema, []byte("operationId: "+tc.operation+"\n")) {
			t.Fatalf("operation missing from OpenAPI: %s", tc.operation)
		}
		t.Run(tc.operation, func(t *testing.T) {
			rootCmd.SetIn(strings.NewReader(tc.body))
			var out, errs bytes.Buffer
			args := append([]string{"--json", "workbench"}, strings.Fields(tc.command)...)
			if code := execute(args, &out, &errs); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"data"`)) {
				t.Fatalf("success: exit=%d out=%s err=%s", code, &out, &errs)
			}
			if tc.operation == "submitWorkbenchTaskOperation" {
				for _, field := range []string{`"sync_state":"conflict"`, `"action":"close"`, `"value":"closed"`} {
					if !bytes.Contains(out.Bytes(), []byte(field)) {
						t.Fatalf("CLI omitted conflict field %s: %s", field, &out)
					}
				}
			}
			select {
			case got := <-requests:
				if want := tc.method + " " + tc.path; got != want {
					t.Fatalf("route: got %s, want %s", got, want)
				}
			default:
				t.Fatal("no HTTP request")
			}
			out.Reset()
			if code := execute(append(args, "unexpected"), &out, &errs); code != 2 || !bytes.Contains(out.Bytes(), []byte(`"INVALID_ARGUMENT"`)) {
				t.Fatalf("invalid args: exit=%d out=%s err=%s", code, &out, &errs)
			}
			select {
			case got := <-requests:
				t.Fatalf("invalid command issued %s", got)
			default:
			}
		})
	}
}
