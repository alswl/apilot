package services

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"

	"github.com/alswl/apilot/core/pkg/integrations"
)

type Remote struct{ socket string }

func NewRemote(socket string) *Remote { return &Remote{socket: socket} }
func (r *Remote) Call(method string, params any) (json.RawMessage, error) {
	if strings.HasPrefix(method, "workbench.") {
		return r.CallWorkbench(context.Background(), method, params)
	}
	return integrations.Call(r.socket, method, params)
}

func (r *Remote) CallWorkbench(ctx context.Context, method string, params any) (json.RawMessage, error) {
	socket := filepath.Join(filepath.Dir(r.socket), "http.sock")
	return integrations.CallWorkbenchHTTPContext(ctx, socket, method, params)
}

func (r *Remote) WatchWorkbenchEvents(ctx context.Context, receive func(common.WorkbenchEvent) error) error {
	return integrations.WatchWorkbenchEvents(ctx, filepath.Join(filepath.Dir(r.socket), "http.sock"), receive)
}

func remoteResult[T any](r *Remote, method string, input any) (T, error) {
	var result T
	raw, err := r.Call(method, input)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}

func (r *Remote) WorkbenchConnections() ([]common.WorkbenchSnapshot, error) {
	result, err := remoteResult[struct {
		Items []common.WorkbenchSnapshot `json:"items"`
	}](r, "workbench.connection.list", nil)
	return result.Items, err
}

func (r *Remote) WorkbenchTasks(q common.WorkbenchQuery) (common.WorkbenchPage, error) {
	return remoteResult[common.WorkbenchPage](r, "workbench.task.list", q)
}

func (r *Remote) QueryWorkbenchTasks(ctx context.Context, input common.WorkbenchTaskQuery) (common.WorkbenchTaskQueryResult, error) {
	var result common.WorkbenchTaskQueryResult
	raw, err := r.CallWorkbench(ctx, "workbench.task.query", input)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}

func (r *Remote) WorkbenchTask(id string) (common.WorkbenchTask, error) {
	return remoteResult[common.WorkbenchTask](r, "workbench.task.show", map[string]string{"id": id})
}

func (r *Remote) SaveWorkbench(c common.WorkbenchConnection) (common.WorkbenchSnapshot, error) {
	return remoteResult[common.WorkbenchSnapshot](r, "workbench.connection.save", c)
}

func (r *Remote) SyncWorkbench(id string) error {
	_, err := r.Call("workbench.sync.run", map[string]string{"id": id})
	return err
}

func (r *Remote) PauseWorkbench(id string, version int64, paused bool) error {
	_, err := remoteResult[common.WorkbenchSnapshot](r, "workbench.connection.pause", map[string]any{"id": id, "version": version, "paused": paused})
	return err
}

func (r *Remote) CancelWorkbenchSync(id string, attemptID int64) error {
	_, err := remoteResult[common.WorkbenchSnapshot](r, "workbench.sync.cancel", map[string]any{"id": id, "attempt_id": attemptID})
	return err
}

func (r *Remote) WriteWorkbenchTask(input common.WorkbenchTaskWrite) (common.WorkbenchTaskReceipt, error) {
	method := "workbench.task.create"
	if input.SourceObjectID != "" {
		method = "workbench.task.reopen"
		if input.State == "done" {
			method = "workbench.task.complete"
		}
	}
	return remoteResult[common.WorkbenchTaskReceipt](r, method, input)
}

func (r *Remote) WorkbenchTaskOperationReceipt(requestID string) (common.WorkbenchTaskReceipt, error) {
	return remoteResult[common.WorkbenchTaskReceipt](r, "workbench.operation.show", map[string]string{"id": requestID})
}

func (r *Remote) SubmitWorkbenchTaskOperation(input common.WorkbenchTaskWrite) (common.WorkbenchTaskReceipt, error) {
	return remoteResult[common.WorkbenchTaskReceipt](r, "workbench.operation.submit", input)
}

func (r *Remote) ReconcileWorkbenchTaskOperation(requestID string) (common.WorkbenchTaskReceipt, error) {
	return remoteResult[common.WorkbenchTaskReceipt](r, "workbench.operation.reconcile", map[string]string{"id": requestID})
}

func (r *Remote) ConfigureWorkbench(c common.WorkbenchConnection, bridge common.WorkbenchBridgeInput) (common.WorkbenchSnapshot, error) {
	return remoteResult[common.WorkbenchSnapshot](r, "workbench.connection.save", struct {
		common.WorkbenchConnection
		Endpoint     string `json:"bridge_endpoint,omitempty"`
		Token        string `json:"bridge_token,omitempty"`
		Certificate  string `json:"bridge_certificate,omitempty"`
		WriteEnabled *bool  `json:"write_enabled,omitempty"`
	}{c, bridge.Endpoint, bridge.Token, bridge.Certificate, bridge.WriteEnabled})
}

func (r *Remote) ConfigureWorkbenchGitHub(c common.WorkbenchConnection, token string, writeEnabled bool) (common.WorkbenchSnapshot, error) {
	return remoteResult[common.WorkbenchSnapshot](r, "workbench.connection.save", struct {
		common.WorkbenchConnection
		GitHubToken  string `json:"github_token,omitempty"`
		WriteEnabled bool   `json:"write_enabled"`
	}{c, token, writeEnabled})
}

func (r *Remote) StartGitHubAuthorization() (GitHubAuthorizationStatus, error) {
	return remoteResult[GitHubAuthorizationStatus](r, "workbench.github.authorization.start", nil)
}

func (r *Remote) WorkbenchGitHubReviewRequests(ctx context.Context) (common.WorkbenchGitHubReviewRequests, error) {
	var result common.WorkbenchGitHubReviewRequests
	raw, err := r.CallWorkbench(ctx, "workbench.github.review.list", nil)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}

func (r *Remote) ListGitHubRepositories(authorizationID string) ([]string, error) {
	result, err := remoteResult[struct {
		Items []string `json:"items"`
	}](r, "workbench.github.repository.list", map[string]string{"authorization_id": authorizationID})
	return result.Items, err
}

func (r *Remote) GitHubAuthorizationStatus(id string) (GitHubAuthorizationStatus, error) {
	return remoteResult[GitHubAuthorizationStatus](r, "workbench.github.authorization.show", map[string]string{"id": id})
}

func (r *Remote) CancelGitHubAuthorization(id string) error {
	_, err := remoteResult[struct{}](r, "workbench.github.authorization.cancel", map[string]string{"id": id})
	return err
}

func (r *Remote) ConfigureWorkbenchGitHubAuthorized(c common.WorkbenchConnection, id string, writeEnabled bool) (common.WorkbenchSnapshot, error) {
	return remoteResult[common.WorkbenchSnapshot](r, "workbench.connection.save", struct {
		common.WorkbenchConnection
		AuthorizationID string `json:"github_authorization_id"`
		WriteEnabled    bool   `json:"write_enabled"`
	}{c, id, writeEnabled})
}

func (r *Remote) WorkbenchNotes(id string) ([]common.WorkbenchNote, error) {
	result, err := remoteResult[struct {
		Items []common.WorkbenchNote `json:"items"`
	}](r, "workbench.connection.notes", map[string]string{"id": id})
	return result.Items, err
}

func (r *Remote) WorkbenchHeadings(id, path string) ([]common.WorkbenchHeading, error) {
	result, err := remoteResult[struct {
		Items []common.WorkbenchHeading `json:"items"`
	}](r, "workbench.connection.headings", map[string]string{"id": id, "note_path": path})
	return result.Items, err
}

func (r *Remote) WorkbenchPlugins() ([]WorkbenchPlugin, error) {
	result, err := remoteResult[struct {
		Items []WorkbenchPlugin `json:"items"`
	}](r, "workbench.plugin.list", nil)
	return result.Items, err
}

func (r *Remote) SetWorkbenchPluginEnabled(id string, enabled bool) error {
	_, err := r.Call("workbench.plugin.set", map[string]any{"id": id, "enabled": enabled})
	return err
}

func (r *Remote) RemoveWorkbench(id string) error {
	_, err := r.Call("workbench.connection.remove", map[string]string{"id": id})
	return err
}

func (r *Remote) WorkbenchSyncDetails(id string) (common.WorkbenchSnapshot, error) {
	return remoteResult[common.WorkbenchSnapshot](r, "workbench.sync.show", map[string]string{"id": id})
}

func (r *Remote) WorkbenchLabels() ([]common.WorkbenchLabel, error) {
	result, err := remoteResult[struct {
		Items []common.WorkbenchLabel `json:"items"`
	}](r, "workbench.label.list", nil)
	return result.Items, err
}
func (r *Remote) SetWorkbenchLabels(taskID string, names []string) error {
	_, err := r.Call("workbench.label.set", map[string]any{"id": taskID, "names": names})
	return err
}
func (r *Remote) WorkbenchAnnotations(taskID string) ([]common.WorkbenchAnnotation, error) {
	result, err := remoteResult[struct {
		Items []common.WorkbenchAnnotation `json:"items"`
	}](r, "workbench.annotation.list", map[string]string{"id": taskID})
	return result.Items, err
}
func (r *Remote) PutWorkbenchAnnotation(taskID, annotationID, body string) error {
	_, err := r.Call("workbench.annotation.put", map[string]string{"id": taskID, "annotation_id": annotationID, "body": body})
	return err
}
func (r *Remote) DeleteWorkbenchAnnotation(taskID, annotationID string) error {
	_, err := r.Call("workbench.annotation.delete", map[string]string{"id": taskID, "annotation_id": annotationID})
	return err
}
