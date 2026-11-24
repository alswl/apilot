package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
)

type workbenchTaskDetailsInput struct {
	ID      string `path:"id" minLength:"1"`
	Segment string `query:"segment" enum:"fields,description,comments,attachments,links,history" default:"fields"`
	Page    string `query:"page" maxLength:"2048"`
}

type workbenchMappingRequest struct {
	ID   string `path:"id" minLength:"1"`
	Body common.WorkbenchMappingInput
}

func registerWorkbenchConnectorTasksHTTP(api huma.API, s *services.Service) {
	errors := []int{400, 403, 404, 409, 503}
	registerWorkbenchContext(api, http.MethodGet, workbenchPath+"/tasks/{id}/detail", "getWorkbenchTaskDetail", "Read a task detail segment", "Tasks", errors,
		func(ctx context.Context, in *workbenchTaskDetailsInput) (common.WorkbenchTaskDetail, error) {
			return s.WorkbenchConnectorTaskDetail(ctx, in.ID, in.Segment, in.Page)
		})
	registerWorkbenchContext(api, http.MethodGet, workbenchPath+"/tasks/{id}/edit-metadata", "getWorkbenchTaskEditMetadata", "Read editable fields and state actions", "Tasks", errors,
		func(ctx context.Context, in *workbenchID) (protocol.EditMetadataResult, error) {
			return s.WorkbenchEditMetadata(ctx, in.ID, "")
		})
	registerWorkbenchContext(api, http.MethodGet, workbenchPath+"/connections/{id}/edit-metadata", "getWorkbenchScopeEditMetadata", "Read scope task creation capability", "Tasks", errors,
		func(ctx context.Context, in *workbenchID) (protocol.EditMetadataResult, error) {
			return s.WorkbenchEditMetadata(ctx, "", in.ID)
		})
	registerWorkbenchContext(api, http.MethodGet, workbenchPath+"/connections/{id}/fields", "getWorkbenchScopeFields", "Read native scope field definitions", "Connections", errors,
		func(ctx context.Context, in *workbenchID) (protocol.FieldCatalog, error) {
			return s.WorkbenchConnectorFields(ctx, in.ID)
		})
	registerWorkbenchContext(api, http.MethodGet, workbenchPath+"/connections/{id}/mapping", "getWorkbenchScopeMapping", "Read a scope mapping", "Connections", errors,
		func(ctx context.Context, in *workbenchID) (common.WorkbenchMappingResult, error) {
			return s.WorkbenchConnectorMapping(ctx, in.ID, nil, false)
		})
	registerWorkbenchContext(api, http.MethodPost, workbenchPath+"/connections/{id}/mapping/preview", "previewWorkbenchScopeMapping", "Preview a proposed scope mapping", "Connections", errors,
		func(ctx context.Context, in *workbenchMappingRequest) (common.WorkbenchMappingResult, error) {
			return s.WorkbenchConnectorMapping(ctx, in.ID, &in.Body, false)
		})
	registerWorkbenchContext(api, http.MethodPut, workbenchPath+"/connections/{id}/mapping", "saveWorkbenchScopeMapping", "Save a scope mapping revision", "Connections", errors,
		func(ctx context.Context, in *workbenchMappingRequest) (common.WorkbenchMappingResult, error) {
			return s.WorkbenchConnectorMapping(ctx, in.ID, &in.Body, true)
		})
}
