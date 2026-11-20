package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func CallWorkbenchHTTP(socket, method string, params any) (json.RawMessage, error) {
	dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		},
	}
	client, err := NewClient("http://apilot", WithHTTPClient(&http.Client{Transport: transport, Timeout: 3 * time.Second}),
		WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
			request.Header.Set(common.WorkbenchAPIVersionHeader, common.WorkbenchAPIVersion)
			return nil
		}))
	if err != nil {
		return nil, err
	}
	response, err := requestWorkbenchHTTP(client, method, params)
	if err != nil {
		var domain *common.Error
		switch {
		case errors.As(err, &domain):
			return nil, domain
		case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ECONNREFUSED):
			return nil, common.Err("APP_NOT_RUNNING", "Start the Apilot local server and retry")
		case errors.Is(err, context.DeadlineExceeded):
			return nil, common.Err("IPC_TIMEOUT", "Apilot HTTP service did not respond in time")
		default:
			return nil, common.Err("IPC_UNAVAILABLE", err.Error())
		}
	}
	defer response.Body.Close()
	const maxResponse = 16 << 20
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, common.Err("IPC_TIMEOUT", "Apilot HTTP service did not respond in time")
		}
		return nil, common.Err("IPC_UNAVAILABLE", "Could not read Apilot HTTP response")
	}
	if len(payload) > maxResponse || !json.Valid(payload) {
		return nil, common.Err("IPC_UNAVAILABLE", "Invalid Apilot HTTP response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, decodeWorkbenchProblem(response.StatusCode, payload)
	}
	return payload, nil
}

func requestWorkbenchHTTP(client *Client, method string, params any) (*http.Response, error) {
	ctx := context.Background()
	switch method {
	case "workbench.health":
		return client.Health(ctx)
	case "workbench.connector.list":
		return client.ListWorkbenchConnectors(ctx)
	case "workbench.connection.list":
		return client.ListWorkbenchConnections(ctx)
	}
	input, err := json.Marshal(params)
	if err != nil {
		return nil, common.Err("INVALID_ARGUMENT", "Invalid workbench request")
	}
	switch method {
	case "workbench.plugin.list":
		return client.ListWorkbenchPlugins(ctx)
	case "workbench.task.list":
		var q common.WorkbenchQuery
		if err := json.Unmarshal(input, &q); err != nil {
			return nil, common.Err("INVALID_ARGUMENT", "Invalid task query")
		}
		mapped := &ListWorkbenchTasksParams{}
		if q.Connection != "" {
			mapped.Connection = &q.Connection
		}
		if q.State != "" {
			state := ListWorkbenchTasksParamsState(q.State)
			mapped.State = &state
		}
		if q.Revision != 0 {
			mapped.Revision = &q.Revision
		}
		if q.Limit != 0 {
			limit := int64(q.Limit)
			mapped.Limit = &limit
		}
		if q.Offset != 0 {
			offset := int64(q.Offset)
			mapped.Offset = &offset
		}
		return client.ListWorkbenchTasks(ctx, mapped)
	case "workbench.connection.save":
		return client.SaveWorkbenchConnectionWithBody(ctx, "application/json", bytes.NewReader(input))
	case "workbench.task.create":
		return client.CreateWorkbenchTaskWithBody(ctx, "application/json", bytes.NewReader(input))
	case "workbench.task.complete", "workbench.task.reopen":
		var task common.WorkbenchTaskWrite
		if err := json.Unmarshal(input, &task); err != nil || task.SourceObjectID == "" {
			return nil, common.Err("INVALID_ARGUMENT", "Task source object ID is required")
		}
		task.State = "open"
		if method == "workbench.task.complete" {
			task.State = "done"
		}
		body, err := json.Marshal(task)
		if err != nil {
			return nil, err
		}
		return client.SetWorkbenchTaskStateWithBody(ctx, task.SourceObjectID, "application/json", bytes.NewReader(body))
	case "workbench.task.import":
		return client.ImportWorkbenchTasksWithBody(ctx, "application/json", bytes.NewReader(input))
	}
	var p struct {
		ID        string `json:"id"`
		Version   int64  `json:"version"`
		Paused    bool   `json:"paused"`
		Enabled   bool   `json:"enabled"`
		NotePath  string `json:"note_path"`
		AttemptID int64  `json:"attempt_id"`
	}
	if err := json.Unmarshal(input, &p); err != nil || p.ID == "" {
		return nil, common.Err("INVALID_ARGUMENT", "A workbench ID is required")
	}
	switch method {
	case "workbench.task.show":
		return client.GetWorkbenchTask(ctx, p.ID)
	case "workbench.connection.show":
		return client.GetWorkbenchConnection(ctx, p.ID)
	case "workbench.connection.remove":
		return client.RemoveWorkbenchConnection(ctx, p.ID)
	case "workbench.sync.show":
		return client.GetWorkbenchSync(ctx, p.ID)
	case "workbench.plugin.set":
		return client.SetWorkbenchPluginEnabled(ctx, p.ID, SetWorkbenchPluginEnabledJSONRequestBody{Enabled: p.Enabled})
	case "workbench.connection.notes":
		return client.ListWorkbenchNotes(ctx, p.ID)
	case "workbench.connection.headings":
		return client.ListWorkbenchHeadings(ctx, p.ID, &ListWorkbenchHeadingsParams{NotePath: p.NotePath})
	case "workbench.connection.pause":
		return client.PauseWorkbenchConnection(ctx, p.ID, PauseWorkbenchConnectionJSONRequestBody{Paused: p.Paused, Version: p.Version})
	case "workbench.sync.cancel":
		return client.CancelWorkbenchSync(ctx, p.ID, p.AttemptID)
	case "workbench.sync.run":
		return client.RunWorkbenchSync(ctx, p.ID)
	default:
		return nil, common.Err("INVALID_ARGUMENT", "Unknown workbench method")
	}
}

func decodeWorkbenchProblem(status int, payload []byte) *common.Error {
	var problem ErrorModel
	if err := json.Unmarshal(payload, &problem); err != nil {
		return common.Err("IPC_UNAVAILABLE", "Invalid Apilot HTTP error response")
	}
	code := "INTERNAL"
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusRequestEntityTooLarge:
		code = "INVALID_ARGUMENT"
	case http.StatusNotFound:
		code = "NOT_FOUND"
	case http.StatusConflict:
		code = "CONFLICT"
	case http.StatusServiceUnavailable:
		code = "IPC_UNAVAILABLE"
	}
	if problem.Errors != nil {
		for _, detail := range *problem.Errors {
			if detail.Location != nil && *detail.Location == "code" && detail.Message != nil {
				code = *detail.Message
				break
			}
		}
	}
	message := http.StatusText(status)
	if problem.Detail != nil {
		message = *problem.Detail
	}
	return common.Err(code, message)
}
