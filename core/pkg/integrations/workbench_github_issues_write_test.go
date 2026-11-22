package integrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestGitHubIssueStateWriteUsesOnlyStateFields(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPatch || r.URL.Path != "/repos/a/b/issues/7" || len(input) != 2 || input["state"] != "closed" || input["state_reason"] != "completed" {
			t.Errorf("unexpected request: %s %s %#v", r.Method, r.URL.Path, input)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"number":7,"title":"Example","state":"closed","state_reason":"completed"}`))
	}))
	defer server.Close()
	client, err := NewWorkbenchGitHubClient(server.URL, "secret", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	task, err := client.WriteIssue(context.Background(), common.WorkbenchConnection{Repository: "a/b"}, 7, "", "", "done")
	if err != nil || task.SourceKey != "issue:42" || task.State != "done" {
		t.Fatalf("unexpected result: %+v %v", task, err)
	}
}
