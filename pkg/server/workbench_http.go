package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const workbenchPath = "/v1/workbench"

type workbenchBody[T any] struct{ Body T }
type workbenchItems[T any] struct {
	Items []T `json:"items"`
}
type workbenchID struct {
	ID string `path:"id" minLength:"1"`
}
type workbenchPluginInput struct {
	ID   string `path:"id" minLength:"1"`
	Body struct {
		Enabled bool `json:"enabled"`
	}
}

type workbenchPauseInput struct {
	ID   string `path:"id" minLength:"1"`
	Body struct {
		Paused  bool  `json:"paused"`
		Version int64 `json:"version" minimum:"1"`
	}
}
type workbenchCancelInput struct {
	ID        string `path:"id" minLength:"1"`
	AttemptID int64  `path:"attempt_id" minimum:"1"`
}
type workbenchResultID struct {
	ID string `json:"id"`
}
type workbenchSyncResult struct {
	ID        string `json:"id"`
	AttemptID int64  `json:"attempt_id"`
}
type workbenchHeadingsInput struct {
	ID       string `path:"id" minLength:"1"`
	NotePath string `query:"note_path" required:"true"`
}

type workbenchSetTaskState struct {
	ID   string `path:"id" minLength:"1"`
	Body common.WorkbenchTaskWrite
}
type workbenchTaskList struct {
	Connection string `query:"connection"`
	State      string `query:"state" enum:"open,in_progress,done,cancelled,unknown"`
	Offset     int    `query:"offset" minimum:"0"`
	Limit      int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
	Revision   int64  `query:"revision" minimum:"0"`
}
type workbenchConnectionInput struct {
	common.WorkbenchBridgeInput
	ID         string            `json:"id,omitempty"`
	Kind       string            `json:"kind" enum:"obsidian_tasks,github_issues"`
	Name       string            `json:"name" minLength:"1" maxLength:"256"`
	ReadMethod string            `json:"read_method,omitempty"`
	Vault      string            `json:"vault,omitempty"`
	Scope      string            `json:"scope,omitempty"`
	Repository string            `json:"repository,omitempty"`
	Mode       string            `json:"mode,omitempty"`
	Statuses   map[string]string `json:"statuses,omitempty"`
	Version    int64             `json:"version,omitempty" minimum:"0"`
}

type workbenchHealth struct {
	Status string `json:"status"`
}

func workbenchRouter(s *services.Service) (http.Handler, huma.API) {
	router := chi.NewRouter()
	router.Use(middleware.RequestID, workbenchRequestLog, middleware.Recoverer, workbenchAPIVersion)
	config := huma.DefaultConfig("Apilot Local Workbench", "1.0.0")
	config.Transformers = nil
	config.Servers = []*huma.Server{{URL: "http://apilot", Description: "HTTP over the Apilot Unix domain socket"}}
	api := humachi.New(router, config)
	registerWorkbenchHTTP(api, s)
	return router, api
}

func WorkbenchOpenAPI() ([]byte, error) {
	_, api := workbenchRouter(nil)
	return api.OpenAPI().YAML()
}

func registerWorkbenchHTTP(api huma.API, s *services.Service) {
	registerWorkbench(api, http.MethodGet, "/health", "health", "Check whether the HTTP service is listening", "System", nil,
		func(_ *struct{}) (workbenchHealth, error) { return workbenchHealth{"ok"}, nil })
	registerWorkbench(api, http.MethodGet, "/ready", "ready", "Check whether the workbench database is readable", "System", []int{503},
		func(_ *struct{}) (workbenchHealth, error) {
			if _, err := s.WorkbenchTasks(common.WorkbenchQuery{Limit: 1}); err != nil {
				return workbenchHealth{}, huma.Error503ServiceUnavailable("Workbench database is unavailable")
			}
			return workbenchHealth{"ready"}, nil
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/plugins", "listWorkbenchPlugins", "List source plugins", "Connectors", nil,
		func(_ *struct{}) (workbenchItems[services.WorkbenchPlugin], error) {
			items, err := s.WorkbenchPlugins()
			return workbenchItems[services.WorkbenchPlugin]{items}, err
		})
	registerWorkbench(api, http.MethodPut, workbenchPath+"/plugins/{id}/enabled", "setWorkbenchPluginEnabled", "Set plugin enabled state", "Connectors", []int{400, 409},
		func(in *workbenchPluginInput) (services.WorkbenchPlugin, error) {
			return s.SetWorkbenchPluginEnabled(in.ID, in.Body.Enabled)
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/connectors", "listWorkbenchConnectors", "List registered connectors", "Connectors", nil,
		func(_ *struct{}) (workbenchItems[services.WorkbenchConnector], error) {
			items, err := s.WorkbenchConnectors()
			return workbenchItems[services.WorkbenchConnector]{items}, err
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/connections", "listWorkbenchConnections", "List connections and latest sync attempts", "Connections", nil,
		func(_ *struct{}) (workbenchItems[common.WorkbenchSnapshot], error) {
			items, err := s.WorkbenchConnections()
			return workbenchItems[common.WorkbenchSnapshot]{items}, err
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/connections/{id}", "getWorkbenchConnection", "Read a connection", "Connections", []int{404},
		func(in *workbenchID) (common.WorkbenchSnapshot, error) { return s.WorkbenchSnapshot(in.ID) })
	registerWorkbench(api, http.MethodPut, workbenchPath+"/connections", "saveWorkbenchConnection", "Save a connection", "Connections", []int{400, 404, 409},
		func(in *workbenchBody[workbenchConnectionInput]) (common.WorkbenchSnapshot, error) {
			id, err := s.ConfigureWorkbench(common.WorkbenchConnection{ID: in.Body.ID, Kind: in.Body.Kind, Name: in.Body.Name, Vault: in.Body.Vault, ReadMethod: in.Body.ReadMethod, Scope: in.Body.Scope, Repository: in.Body.Repository, Statuses: in.Body.Statuses, Version: in.Body.Version}, in.Body.WorkbenchBridgeInput)
			if err != nil {
				return common.WorkbenchSnapshot{}, err
			}
			return s.WorkbenchSnapshot(id)
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/connections/{id}/notes", "listWorkbenchNotes", "List in-scope Markdown notes", "Connections", []int{400, 404, 409},
		func(in *workbenchID) (workbenchItems[common.WorkbenchNote], error) {
			items, err := s.WorkbenchNotes(context.Background(), in.ID)
			return workbenchItems[common.WorkbenchNote]{items}, err
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/connections/{id}/headings", "listWorkbenchHeadings", "List heading paths for a note", "Connections", []int{400, 404, 409},
		func(in *workbenchHeadingsInput) (workbenchItems[common.WorkbenchHeading], error) {
			items, err := s.WorkbenchHeadings(in.ID, in.NotePath)
			return workbenchItems[common.WorkbenchHeading]{items}, err
		})
	registerWorkbench(api, http.MethodDelete, workbenchPath+"/connections/{id}", "removeWorkbenchConnection", "Remove a connection", "Connections", []int{404, 409},
		func(in *workbenchID) (workbenchResultID, error) {
			return workbenchResultID{in.ID}, s.RemoveWorkbench(in.ID)
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/connections/{id}/sync", "getWorkbenchSync", "Read sync status and recent attempts", "Sync", []int{404},
		func(in *workbenchID) (common.WorkbenchSnapshot, error) { return s.WorkbenchSyncDetails(in.ID) })
	registerWorkbench(api, http.MethodPost, workbenchPath+"/connections/{id}/sync", "runWorkbenchSync", "Request a refresh", "Sync", []int{400, 404, 409},
		func(in *workbenchID) (workbenchSyncResult, error) {
			attemptID, err := s.RunWorkbenchSync(in.ID)
			return workbenchSyncResult{in.ID, attemptID}, err
		})
	registerWorkbench(api, http.MethodPut, workbenchPath+"/connections/{id}/paused", "pauseWorkbenchConnection", "Set connection pause state", "Connections", []int{400, 404, 409},
		func(in *workbenchPauseInput) (common.WorkbenchSnapshot, error) {
			return s.PauseWorkbench(in.ID, in.Body.Version, in.Body.Paused)
		})
	registerWorkbench(api, http.MethodPost, workbenchPath+"/connections/{id}/sync/{attempt_id}/cancel", "cancelWorkbenchSync", "Cancel a sync attempt", "Sync", []int{404, 409},
		func(in *workbenchCancelInput) (common.WorkbenchSnapshot, error) {
			return s.CancelWorkbenchSync(in.ID, in.AttemptID)
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/tasks", "listWorkbenchTasks", "Search the committed task snapshot", "Tasks", []int{400, 409},
		func(in *workbenchTaskList) (common.WorkbenchPage, error) {
			return s.WorkbenchTasks(common.WorkbenchQuery{Connection: in.Connection, State: in.State, Offset: in.Offset, Limit: in.Limit, Revision: in.Revision})
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/tasks/{id}", "getWorkbenchTask", "Read a task", "Tasks", []int{404},
		func(in *workbenchID) (common.WorkbenchTask, error) { return s.WorkbenchTask(in.ID) })
	registerWorkbench(api, http.MethodPost, workbenchPath+"/tasks", "createWorkbenchTask", "Create a task", "Tasks", []int{400, 404, 409},
		func(in *workbenchBody[common.WorkbenchTaskWrite]) (common.WorkbenchTaskReceipt, error) {
			if in.Body.SourceObjectID != "" {
				return common.WorkbenchTaskReceipt{}, common.Err("INVALID_ARGUMENT", "Creation cannot target an existing task")
			}
			return s.WriteWorkbenchTask(in.Body)
		})
	registerWorkbench(api, http.MethodPost, workbenchPath+"/tasks/{id}/state", "setWorkbenchTaskState", "Set a task state", "Tasks", []int{400, 404, 409},
		func(in *workbenchSetTaskState) (common.WorkbenchTaskReceipt, error) {
			if in.Body.SourceObjectID != "" && in.Body.SourceObjectID != in.ID {
				return common.WorkbenchTaskReceipt{}, common.Err("INVALID_ARGUMENT", "Task ID does not match target")
			}
			in.Body.SourceObjectID = in.ID
			return s.WriteWorkbenchTask(in.Body)
		})
	registerWorkbench(api, http.MethodPost, workbenchPath+"/tasks/import", "importWorkbenchTasks", "Replace the tasks in a Local connection", "Tasks", []int{400, 409},
		func(in *workbenchBody[common.WorkbenchImport]) (common.WorkbenchSnapshot, error) {
			return s.ImportWorkbench(in.Body)
		})
}

func registerWorkbench[I, O any](api huma.API, method, path, id, summary, tag string, statuses []int, call func(*I) (O, error)) {
	huma.Register(api, huma.Operation{
		Method: method, Path: path, OperationID: id, Summary: summary, Tags: []string{tag}, Errors: append(statuses, http.StatusInternalServerError),
		MaxBodyBytes: 1 << 20,
	}, func(_ context.Context, in *I) (*workbenchBody[O], error) {
		result, err := call(in)
		if err != nil {
			return nil, workbenchHTTPError(err)
		}
		return &workbenchBody[O]{Body: result}, nil
	})
}

func workbenchHTTPError(err error) error {
	if problem, ok := err.(huma.StatusError); ok {
		return problem
	}
	domain := common.ProtocolError(err)
	detail := &huma.ErrorDetail{Location: "code", Message: domain.Code}
	switch domain.Code {
	case "NOT_FOUND":
		return huma.Error404NotFound(domain.Message, detail)
	case "INVALID_ARGUMENT":
		return huma.Error400BadRequest(domain.Message, detail)
	case "STALE_REVISION", "CONFLICT":
		return huma.Error409Conflict(domain.Message, detail)
	default:
		return huma.Error500InternalServerError("Workbench request failed", detail)
	}
}

func workbenchAPIVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(common.WorkbenchAPIVersionHeader, common.WorkbenchAPIVersion)
		requested := r.Header.Get(common.WorkbenchAPIVersionHeader)
		if requested != "" && requested != common.WorkbenchAPIVersion {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusConflict)
			problem := huma.Error409Conflict("Unsupported Workbench API version", &huma.ErrorDetail{Location: "code", Message: "UNSUPPORTED_VERSION"})
			_ = json.NewEncoder(w).Encode(problem)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func workbenchRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		started := time.Now()
		next.ServeHTTP(wrapped, r)
		slog.DebugContext(r.Context(), "Workbench HTTP request", "method", r.Method, "route", chi.RouteContext(r.Context()).RoutePattern(), "status", wrapped.Status(), "duration", time.Since(started), "request_id", middleware.GetReqID(r.Context()))
	})
}
