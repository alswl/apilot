package services

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/alswl/apilot/pkg/common"

	"github.com/alswl/apilot/pkg/integrations"
)

type Remote struct{ socket string }

func NewRemote(socket string) *Remote { return &Remote{socket: socket} }
func (r *Remote) Call(method string, params any) (json.RawMessage, error) {
	if strings.HasPrefix(method, "workbench.") {
		socket := filepath.Join(filepath.Dir(r.socket), "http.sock")
		return integrations.CallWorkbenchHTTP(socket, method, params)
	}
	return integrations.Call(r.socket, method, params)
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

func (r *Remote) SaveWorkbench(c common.WorkbenchConnection) (common.WorkbenchSnapshot, error) {
	return remoteResult[common.WorkbenchSnapshot](r, "workbench.connection.save", c)
}

func (r *Remote) SyncWorkbench(id string) error {
	_, err := r.Call("workbench.sync.run", map[string]string{"id": id})
	return err
}
