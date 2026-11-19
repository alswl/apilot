package services

import (
	"context"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/integrations"
)

func (s *Service) workbenchObsidianConnection(id string) (common.WorkbenchSnapshot, error) {
	connection, err := s.WorkbenchSnapshot(id)
	if err != nil {
		return connection, err
	}
	if connection.Kind != common.WorkbenchObsidianTasks {
		return connection, common.Err("INVALID_ARGUMENT", "Connection does not contain Markdown notes")
	}
	if connection.Paused || connection.PluginDisabled {
		return connection, common.Err("CONFLICT", "Connection is paused or disabled")
	}
	return connection, nil
}

func (s *Service) WorkbenchNotes(ctx context.Context, id string) ([]common.WorkbenchNote, error) {
	connection, err := s.workbenchObsidianConnection(id)
	if err != nil {
		return nil, err
	}
	notes, err := integrations.ListWorkbenchObsidianNotes(ctx, connection.WorkbenchConnection)
	if err != nil {
		if connection.ReadMethod == common.WorkbenchObsidianCLI {
			return nil, err
		}
		return nil, common.Err("CONFLICT", "Vault notes could not be read")
	}
	current, err := s.workbenchObsidianConnection(id)
	if err != nil || current.Version != connection.Version {
		return nil, common.Err("STALE_REVISION", "Connection changed while reading notes")
	}
	return notes, nil
}

func (s *Service) WorkbenchHeadings(id, notePath string) ([]common.WorkbenchHeading, error) {
	connection, err := s.workbenchObsidianConnection(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	defer cancel()
	content, err := integrations.ReadWorkbenchObsidianTarget(ctx, connection.WorkbenchConnection, notePath)
	if err != nil {
		if problem, ok := err.(*common.Error); ok {
			return nil, problem
		}
		return nil, common.Err("CONFLICT", "Note could not be read")
	}
	current, err := s.workbenchObsidianConnection(id)
	if err != nil || current.Version != connection.Version {
		return nil, common.Err("STALE_REVISION", "Connection changed while reading headings")
	}
	return integrations.WorkbenchObsidianHeadings(content), nil
}
