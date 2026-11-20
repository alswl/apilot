package services

import "github.com/alswl/apilot/core/pkg/common"

type workbenchMetadataStore interface {
	WorkbenchLabels() ([]common.WorkbenchLabel, error)
	SetWorkbenchLabels(string, []string) error
	WorkbenchAnnotations(string) ([]common.WorkbenchAnnotation, error)
	PutWorkbenchAnnotation(string, string, string) error
	DeleteWorkbenchAnnotation(string, string) error
}

func (s *Service) workbenchMetadataStore() (workbenchMetadataStore, error) {
	store, ok := s.store.(workbenchMetadataStore)
	if !ok {
		return nil, common.Err("INTERNAL", "Task metadata storage unavailable")
	}
	return store, nil
}

func (s *Service) WorkbenchLabels() ([]common.WorkbenchLabel, error) {
	store, err := s.workbenchMetadataStore()
	if err != nil {
		return nil, err
	}
	return store.WorkbenchLabels()
}

func (s *Service) SetWorkbenchLabels(taskID string, names []string) error {
	store, err := s.workbenchMetadataStore()
	if err != nil {
		return err
	}
	return store.SetWorkbenchLabels(taskID, names)
}

func (s *Service) WorkbenchAnnotations(taskID string) ([]common.WorkbenchAnnotation, error) {
	store, err := s.workbenchMetadataStore()
	if err != nil {
		return nil, err
	}
	return store.WorkbenchAnnotations(taskID)
}

func (s *Service) PutWorkbenchAnnotation(taskID, annotationID, body string) error {
	store, err := s.workbenchMetadataStore()
	if err != nil {
		return err
	}
	return store.PutWorkbenchAnnotation(taskID, annotationID, body)
}

func (s *Service) DeleteWorkbenchAnnotation(taskID, annotationID string) error {
	store, err := s.workbenchMetadataStore()
	if err != nil {
		return err
	}
	return store.DeleteWorkbenchAnnotation(taskID, annotationID)
}
