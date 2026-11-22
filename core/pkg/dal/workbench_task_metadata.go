package dal

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

// WorkbenchLabels returns the locally managed labels.
func (s *Store) WorkbenchLabels() ([]common.WorkbenchLabel, error) {
	rows, err := s.db.Query(`SELECT DISTINCT l.id,l.name FROM workbench_labels l
		JOIN workbench_task_labels tl ON tl.label_id=l.id JOIN workbench_objects o ON o.id=tl.task_id
		WHERE NOT EXISTS (SELECT 1 FROM workbench_github_connections g WHERE g.connection_id=o.connection_id AND g.access_state<>'available')
		ORDER BY l.name,l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := []common.WorkbenchLabel{}
	for rows.Next() {
		var label common.WorkbenchLabel
		if err := rows.Scan(&label.ID, &label.Name); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

// WorkbenchAnnotations returns annotations for a currently visible task.
func (s *Store) WorkbenchAnnotations(taskID string) ([]common.WorkbenchAnnotation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := visibleWorkbenchTask(tx, taskID); err != nil {
		return nil, err
	}
	rows, err := tx.Query("SELECT id, task_id, body, created_at, updated_at FROM workbench_task_annotations WHERE task_id=? ORDER BY created_at, id", taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	annotations := []common.WorkbenchAnnotation{}
	for rows.Next() {
		var item common.WorkbenchAnnotation
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Body, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		annotations = append(annotations, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return annotations, tx.Commit()
}

// SetWorkbenchLabels replaces the local label set for a visible task.
func (s *Store) SetWorkbenchLabels(taskID string, names []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := visibleWorkbenchTask(tx, taskID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM workbench_task_labels WHERE task_id=?", taskID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return common.Err("INVALID_ARGUMENT", "Label name cannot be empty")
		}
		if _, err := tx.Exec("INSERT INTO workbench_labels(id,name,created_at) VALUES(?,?,?) ON CONFLICT(name) DO NOTHING", common.ID(), name, now); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO workbench_task_labels(task_id,label_id) SELECT ?,id FROM workbench_labels WHERE name=? ON CONFLICT(task_id,label_id) DO NOTHING", taskID, name); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
		return err
	}
	return tx.Commit()
}

// PutWorkbenchAnnotation creates or updates one annotation on a visible task.
func (s *Store) PutWorkbenchAnnotation(taskID, annotationID, body string) error {
	if strings.TrimSpace(annotationID) == "" || strings.TrimSpace(body) == "" {
		return common.Err("INVALID_ARGUMENT", "Annotation ID and text are required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := visibleWorkbenchTask(tx, taskID); err != nil {
		return err
	}
	var owner string
	err = tx.QueryRow("SELECT task_id FROM workbench_task_annotations WHERE id=?", annotationID).Scan(&owner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && owner != taskID {
		return common.Err("CONFLICT", "Annotation belongs to another task")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.Exec(`INSERT INTO workbench_task_annotations(id,task_id,body,created_at,updated_at) VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET body=excluded.body,updated_at=excluded.updated_at WHERE workbench_task_annotations.body<>excluded.body`, annotationID, taskID, body, now, now)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 {
		if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteWorkbenchAnnotation removes an annotation from a visible task.
func (s *Store) DeleteWorkbenchAnnotation(taskID, annotationID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := visibleWorkbenchTask(tx, taskID); err != nil {
		return err
	}
	result, err := tx.Exec("DELETE FROM workbench_task_annotations WHERE id=? AND task_id=?", annotationID, taskID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 {
		if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func visibleWorkbenchTask(tx *sql.Tx, taskID string) error {
	var exists int
	err := tx.QueryRow("SELECT 1 FROM workbench_objects WHERE id=? AND NOT EXISTS (SELECT 1 FROM workbench_github_connections g WHERE g.connection_id=workbench_objects.connection_id AND g.access_state<>'available')", taskID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return common.Err("NOT_FOUND", "Task not found")
	}
	return err
}

// WorkbenchTaskLabels returns local labels grouped by visible task ID.
func (s *Store) WorkbenchTaskLabels(taskIDs []string) (map[string][]common.WorkbenchLabel, error) {
	return workbenchTaskLabels(s.db, taskIDs)
}

func workbenchTaskLabels(db interface {
	Query(string, ...any) (*sql.Rows, error)
}, taskIDs []string) (map[string][]common.WorkbenchLabel, error) {
	result := make(map[string][]common.WorkbenchLabel, len(taskIDs))
	if len(taskIDs) == 0 {
		return result, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(taskIDs)), ",")
	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		args[i] = id
	}
	rows, err := db.Query("SELECT tl.task_id,l.id,l.name FROM workbench_task_labels tl JOIN workbench_labels l ON l.id=tl.label_id JOIN workbench_objects o ON o.id=tl.task_id WHERE NOT EXISTS (SELECT 1 FROM workbench_github_connections g WHERE g.connection_id=o.connection_id AND g.access_state<>'available') AND tl.task_id IN ("+placeholders+") ORDER BY l.name,l.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var taskID string
		var label common.WorkbenchLabel
		if err := rows.Scan(&taskID, &label.ID, &label.Name); err != nil {
			return nil, err
		}
		result[taskID] = append(result[taskID], label)
	}
	return result, rows.Err()
}
