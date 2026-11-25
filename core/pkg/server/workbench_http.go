package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
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
type workbenchOperationID struct {
	RequestID string `path:"request_id" minLength:"1"`
}
type workbenchPluginImportInput struct {
	Path string `json:"path" minLength:"1"`
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
type workbenchLabelsInput struct {
	ID   string `path:"id" minLength:"1"`
	Body struct {
		Names []string `json:"names"`
	}
}
type workbenchAnnotationInput struct {
	ID           string `path:"id" minLength:"1"`
	AnnotationID string `path:"annotation_id" minLength:"1"`
	Body         struct {
		Body string `json:"body" minLength:"1"`
	}
}
type workbenchAnnotationID struct {
	ID           string `path:"id" minLength:"1"`
	AnnotationID string `path:"annotation_id" minLength:"1"`
}
type workbenchTaskList struct {
	common.WorkbenchQuery
	PriorityFilter string `query:"priority"`
}
type workbenchConnectionInput struct {
	common.WorkbenchBridgeInput
	ID                    string            `json:"id,omitempty"`
	Kind                  string            `json:"kind" enum:"obsidian_tasks,github_issues,github_repository,github-issues,fixture"`
	Name                  string            `json:"name" minLength:"1" maxLength:"256"`
	ReadMethod            *string           `json:"read_method,omitempty" hidden:"true"`
	Vault                 string            `json:"vault,omitempty"`
	Scope                 string            `json:"scope,omitempty"`
	Repository            string            `json:"repository,omitempty"`
	GitHubToken           string            `json:"github_token,omitempty" writeOnly:"true"`
	GitHubAuthorizationID string            `json:"github_authorization_id,omitempty" writeOnly:"true"`
	Mode                  string            `json:"mode,omitempty"`
	Statuses              map[string]string `json:"statuses,omitempty"`
	Version               int64             `json:"version,omitempty" minimum:"0"`
}

type workbenchHealth struct {
	Status string `json:"status"`
}

func workbenchRouter(s *services.Service) (http.Handler, huma.API) {
	return workbenchRouterWithEvents(s, nil)
}

func workbenchRouterWithEvents(s *services.Service, events *workbenchEvents) (http.Handler, huma.API) {
	router := chi.NewRouter()
	router.Use(middleware.RequestID, workbenchRequestLog, middleware.Recoverer, workbenchAPIVersion, redactWorkbenchConnectionErrors)
	config := huma.DefaultConfig("Apilot Local Workbench", "1.0.0")
	config.Transformers = nil
	config.Servers = []*huma.Server{{URL: "http://apilot", Description: "HTTP over the Apilot Unix domain socket"}}
	api := humachi.New(router, config)
	registerWorkbenchHTTP(api, s)
	registerWorkbenchEventHTTP(router, api, events)
	return router, api
}

type workbenchBufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *workbenchBufferedResponse) Header() http.Header { return w.header }
func (w *workbenchBufferedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *workbenchBufferedResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func redactWorkbenchConnectionErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != workbenchPath+"/connections" {
			next.ServeHTTP(w, r)
			return
		}
		buffer := &workbenchBufferedResponse{header: make(http.Header)}
		next.ServeHTTP(buffer, r)
		if buffer.status == 0 {
			buffer.status = http.StatusOK
		}
		body := buffer.body.Bytes()
		if buffer.status >= 400 {
			var value any
			if json.Unmarshal(body, &value) == nil {
				redactWorkbenchSecrets(value)
				if clean, err := json.Marshal(value); err == nil {
					body = clean
					buffer.header.Del("Content-Length")
				}
			}
		}
		for key, values := range buffer.header {
			w.Header()[key] = values
		}
		w.WriteHeader(buffer.status)
		_, _ = w.Write(body)
	})
}

func redactWorkbenchSecrets(value any) {
	switch item := value.(type) {
	case map[string]any:
		delete(item, "github_token")
		delete(item, "github_authorization_id")
		if location, ok := item["location"].(string); ok {
			for _, field := range []string{"body.github_token", "body.github_authorization_id"} {
				if location == field || strings.HasPrefix(location, field+".") || strings.HasPrefix(location, field+"[") {
					delete(item, "value")
				}
			}
		}
		for _, child := range item {
			redactWorkbenchSecrets(child)
		}
	case []any:
		for _, child := range item {
			redactWorkbenchSecrets(child)
		}
	}
}

func WorkbenchOpenAPI() ([]byte, error) {
	_, api := workbenchRouter(nil)
	return api.OpenAPI().YAML()
}

func registerWorkbenchHTTP(api huma.API, s *services.Service) {
	registerWorkbenchConnectorHTTP(api, s)
	registerWorkbenchConnectorTasksHTTP(api, s)
	registerWorkbenchSyncRunHTTP(api, s)
	registerWorkbench(api, http.MethodGet, workbenchPath+"/github-repositories", "listGitHubRepositories", "List authorized GitHub Issues repositories", "Connections", []int{401, 403, 409, 429, 503},
		func(in *struct {
			AuthorizationID string `query:"authorization_id"`
		}) (workbenchItems[string], error) {
			items, err := s.ListGitHubRepositories(in.AuthorizationID)
			return workbenchItems[string]{items}, err
		})
	registerWorkbench(api, http.MethodPost, workbenchPath+"/github-authorizations", "startGitHubAuthorization", "Start GitHub browser authorization", "Connections", []int{400, 409, 503},
		func(_ *struct{}) (services.GitHubAuthorizationStatus, error) { return s.StartGitHubAuthorization() })
	registerWorkbench(api, http.MethodGet, workbenchPath+"/github-authorizations/{id}", "getGitHubAuthorization", "Read GitHub browser authorization status", "Connections", []int{404, 409},
		func(in *workbenchID) (services.GitHubAuthorizationStatus, error) {
			return s.GitHubAuthorizationStatus(in.ID)
		})
	registerWorkbench(api, http.MethodDelete, workbenchPath+"/github-authorizations/{id}", "cancelGitHubAuthorization", "Cancel GitHub browser authorization", "Connections", []int{404, 409},
		func(in *workbenchID) (workbenchResultID, error) {
			return workbenchResultID{in.ID}, s.CancelGitHubAuthorization(in.ID)
		})
	registerWorkbench(api, http.MethodPost, workbenchPath+"/task-operations", "submitWorkbenchTaskOperation", "Accept a task operation", "Tasks", []int{400, 404, 409},
		func(in *workbenchBody[common.WorkbenchTaskWrite]) (common.WorkbenchTaskReceipt, error) {
			return s.SubmitWorkbenchTaskOperation(in.Body)
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/task-operations/{request_id}", "getWorkbenchTaskOperation", "Read a task operation receipt", "Tasks", []int{404},
		func(in *workbenchOperationID) (common.WorkbenchTaskReceipt, error) {
			return s.WorkbenchTaskOperationReceipt(in.RequestID)
		})
	registerWorkbench(api, http.MethodPost, workbenchPath+"/task-operations/{request_id}/reconcile", "reconcileWorkbenchTaskOperation", "Check the original task operation against its source", "Tasks", []int{404, 409},
		func(in *workbenchOperationID) (common.WorkbenchTaskReceipt, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return s.ReconcileWorkbenchTaskOperation(ctx, in.RequestID)
		})
	registerWorkbench(api, http.MethodGet, "/health", "health", "Check whether the HTTP service is listening", "System", nil,
		func(_ *struct{}) (workbenchHealth, error) { return workbenchHealth{"ok"}, nil })
	registerWorkbench(api, http.MethodGet, "/ready", "ready", "Check whether the workbench database is readable", "System", []int{503},
		func(_ *struct{}) (workbenchHealth, error) {
			if _, err := s.WorkbenchTasks(common.WorkbenchQuery{Limit: 1}); err != nil {
				return workbenchHealth{}, huma.Error503ServiceUnavailable("Workbench database is unavailable")
			}
			return workbenchHealth{"ready"}, nil
		})
	registerWorkbenchContext(api, http.MethodPost, workbenchPath+"/plugins/import", "importWorkbenchConnector", "Install or upgrade a signed local Connector package", "Connectors", []int{400, 409},
		func(ctx context.Context, in *workbenchBody[workbenchPluginImportInput]) (services.WorkbenchPlugin, error) {
			return s.ImportWorkbenchConnector(ctx, in.Body.Path)
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
	registerWorkbench(api, http.MethodPut, workbenchPath+"/connections", "saveWorkbenchConnection", "Save a connection", "Connections", []int{400, 401, 403, 404, 409, 429, 503},
		func(in *workbenchBody[workbenchConnectionInput]) (common.WorkbenchSnapshot, error) {
			if in.Body.ID != "" {
				prior, err := s.WorkbenchSnapshot(in.Body.ID)
				if err != nil {
					return common.WorkbenchSnapshot{}, err
				}
				if prior.ParentConnectionID != "" {
					if in.Body.GitHubToken != "" || in.Body.GitHubAuthorizationID != "" {
						return common.WorkbenchSnapshot{}, common.Err("INVALID_ARGUMENT", "Managed Connector owns source authorization")
					}
					write := prior.WriteEnabled
					if in.Body.WriteEnabled != nil {
						write = *in.Body.WriteEnabled
					}
					return s.ConfigureWorkbenchConnectorScope(in.Body.ID, in.Body.Kind, in.Body.Name, in.Body.Version, write)
				}
			}
			readMethod := ""
			if in.Body.ReadMethod != nil {
				readMethod = *in.Body.ReadMethod
			} else if in.Body.ID != "" {
				prior, err := s.WorkbenchSnapshot(in.Body.ID)
				if err != nil {
					return common.WorkbenchSnapshot{}, err
				}
				readMethod = prior.ReadMethod
			}
			if in.Body.Kind == common.WorkbenchGitHubRepository {
				connection := common.WorkbenchConnection{ID: in.Body.ID, Kind: in.Body.Kind, Name: in.Body.Name, Repository: in.Body.Repository, Version: in.Body.Version}
				if in.Body.GitHubToken != "" && in.Body.GitHubAuthorizationID != "" {
					return common.WorkbenchSnapshot{}, common.Err("INVALID_ARGUMENT", "Choose one GitHub authorization method")
				}
				var id string
				var err error
				if in.Body.GitHubAuthorizationID != "" {
					id, err = s.ConfigureWorkbenchGitHubAuthorized(connection, in.Body.GitHubAuthorizationID, in.Body.WriteEnabled)
				} else {
					id, err = s.ConfigureWorkbenchGitHub(connection, in.Body.GitHubToken, in.Body.WriteEnabled)
				}
				if err != nil {
					return common.WorkbenchSnapshot{}, err
				}
				return s.WorkbenchSnapshot(id)
			}
			if in.Body.GitHubToken != "" || in.Body.GitHubAuthorizationID != "" {
				return common.WorkbenchSnapshot{}, common.Err("INVALID_ARGUMENT", "GitHub token requires a GitHub repository connection")
			}
			id, err := s.ConfigureWorkbench(common.WorkbenchConnection{ID: in.Body.ID, Kind: in.Body.Kind, Name: in.Body.Name, Vault: in.Body.Vault, ReadMethod: readMethod, Scope: in.Body.Scope, Repository: in.Body.Repository, Statuses: in.Body.Statuses, Version: in.Body.Version}, in.Body.WorkbenchBridgeInput)
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
	registerWorkbench(api, http.MethodPost, workbenchPath+"/connections/{id}/sync", "runWorkbenchSync", "Request a refresh", "Sync", []int{400, 404, 409, 429},
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
			query := in.WorkbenchQuery
			if in.PriorityFilter != "" {
				priority, err := strconv.Atoi(in.PriorityFilter)
				if err != nil {
					return common.WorkbenchPage{}, common.Err("INVALID_ARGUMENT", "Invalid priority filter")
				}
				query.Priority = &priority
			}
			return s.WorkbenchTasks(query)
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/labels", "listWorkbenchLabels", "List local task labels", "Tasks", nil,
		func(_ *struct{}) (workbenchItems[common.WorkbenchLabel], error) {
			items, err := s.WorkbenchLabels()
			return workbenchItems[common.WorkbenchLabel]{items}, err
		})
	registerWorkbench(api, http.MethodPut, workbenchPath+"/tasks/{id}/labels", "setWorkbenchLabels", "Set local task labels", "Tasks", []int{400, 404},
		func(in *workbenchLabelsInput) (workbenchResultID, error) {
			err := s.SetWorkbenchLabels(in.ID, in.Body.Names)
			return workbenchResultID{in.ID}, err
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/tasks/{id}/annotations", "listWorkbenchAnnotations", "List local task annotations", "Tasks", []int{404},
		func(in *workbenchID) (workbenchItems[common.WorkbenchAnnotation], error) {
			items, err := s.WorkbenchAnnotations(in.ID)
			return workbenchItems[common.WorkbenchAnnotation]{items}, err
		})
	registerWorkbench(api, http.MethodPut, workbenchPath+"/tasks/{id}/annotations/{annotation_id}", "putWorkbenchAnnotation", "Save a local task annotation", "Tasks", []int{400, 404, 409},
		func(in *workbenchAnnotationInput) (workbenchResultID, error) {
			err := s.PutWorkbenchAnnotation(in.ID, in.AnnotationID, in.Body.Body)
			return workbenchResultID{in.AnnotationID}, err
		})
	registerWorkbench(api, http.MethodDelete, workbenchPath+"/tasks/{id}/annotations/{annotation_id}", "deleteWorkbenchAnnotation", "Delete a local task annotation", "Tasks", []int{404},
		func(in *workbenchAnnotationID) (workbenchResultID, error) {
			err := s.DeleteWorkbenchAnnotation(in.ID, in.AnnotationID)
			return workbenchResultID{in.AnnotationID}, err
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
	registerWorkbenchContext(api, method, path, id, summary, tag, statuses, func(_ context.Context, in *I) (O, error) { return call(in) })
}

func registerWorkbenchContext[I, O any](api huma.API, method, path, id, summary, tag string, statuses []int, call func(context.Context, *I) (O, error)) {
	huma.Register(api, huma.Operation{
		Method: method, Path: path, OperationID: id, Summary: summary, Tags: []string{tag}, Errors: append(statuses, http.StatusInternalServerError),
		MaxBodyBytes: 1 << 20,
	}, func(ctx context.Context, in *I) (*workbenchBody[O], error) {
		result, err := call(ctx, in)
		if err != nil {
			return nil, workbenchHTTPError(err)
		}
		return &workbenchBody[O]{Body: publicWorkbenchResult(result)}, nil
	})
}

func publicWorkbenchSnapshot(snapshot common.WorkbenchSnapshot) common.WorkbenchSnapshot {
	snapshot.ReadMethod = ""
	snapshot.BridgeConfigured = false
	return snapshot
}

func publicWorkbenchResult[O any](result O) O {
	switch value := any(result).(type) {
	case common.WorkbenchSnapshot:
		return any(publicWorkbenchSnapshot(value)).(O)
	case workbenchItems[common.WorkbenchSnapshot]:
		for i := range value.Items {
			value.Items[i] = publicWorkbenchSnapshot(value.Items[i])
		}
		return any(value).(O)
	default:
		return result
	}
}

func workbenchHTTPError(err error) error {
	if problem, ok := err.(huma.StatusError); ok {
		return problem
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		err = common.Err("SOURCE_UNAVAILABLE", "Workbench request could not finish")
	}
	domain := common.ProtocolError(err)
	details := []error{&huma.ErrorDetail{Location: "code", Message: domain.Code}}
	for _, key := range []string{"connection_id", "retry_after", "rate_limit_reset", "github_installation_required"} {
		if value, ok := domain.Details[key]; ok {
			switch value.(type) {
			case string, int, int64, float64, bool:
				details = append(details, &huma.ErrorDetail{Location: key, Value: value})
			}
		}
	}
	switch domain.Code {
	case "NOT_FOUND", "ISSUE_DELETED":
		return huma.Error404NotFound(domain.Message, details...)
	case "INVALID_ARGUMENT":
		return huma.Error400BadRequest(domain.Message, details...)
	case "STALE_REVISION", "CONFLICT":
		return huma.Error409Conflict(domain.Message, details...)
	case "RATE_LIMITED":
		return huma.Error429TooManyRequests(domain.Message, details...)
	case "UNAUTHORIZED":
		return huma.Error401Unauthorized(domain.Message, details...)
	case "FORBIDDEN":
		return huma.Error403Forbidden(domain.Message, details...)
	case "SOURCE_UNAVAILABLE":
		return huma.Error503ServiceUnavailable(domain.Message, details...)
	default:
		return huma.Error500InternalServerError("Workbench request failed", details...)
	}
}

func workbenchAPIVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, workbenchPath+"/") {
			w.Header().Set("Cache-Control", "no-store")
		}
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
