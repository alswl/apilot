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
		{"watchWorkbenchEvents", "GET", "/v1/workbench/events", "watch --once", ``},
		{"startGitHubAuthorization", "POST", "/v1/workbench/github-authorizations", "github authorization start", ``},
		{"cancelGitHubAuthorization", "DELETE", "/v1/workbench/github-authorizations/item", "github authorization cancel item", ``},
		{"getGitHubAuthorization", "GET", "/v1/workbench/github-authorizations/item", "github authorization show item", ``},
		{"listGitHubRepositories", "GET", "/v1/workbench/github-repositories", "github repositories --authorization item", ``},
		{"listWorkbenchLabels", "GET", "/v1/workbench/labels", "label list", ``},
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
