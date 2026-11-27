package dal

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"

	"github.com/alswl/apilot/core/pkg/common"
)

// WorkbenchQuerySnapshot reads task summaries, labels and source metadata at a
// single revision. The boundary-only read supports revalidation after evaluation.
func (s *Store) WorkbenchQuerySnapshot(ctx context.Context, boundaryOnly bool) (int64, []common.WorkbenchQueryScope, []common.WorkbenchTask, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, nil, nil, err
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, "SELECT revision FROM workbench_revision").Scan(&revision); err != nil {
		return 0, nil, nil, err
	}
	sources, err := workbenchQuerySources(ctx, tx)
	if err != nil || boundaryOnly {
		return revision, sources, nil, err
	}
	tasks, err := workbenchQueryTasks(ctx, tx)
	return revision, sources, tasks, err
}

func workbenchQuerySources(ctx context.Context, tx *sql.Tx) ([]common.WorkbenchQueryScope, error) {
	rows, err := tx.QueryContext(ctx, `SELECT c.config,c.version,coalesce(g.account_login,''),coalesce(g.account_id,0),coalesce(p.auth_generation,0)
		FROM workbench_connections c
		LEFT JOIN workbench_github_connections g ON g.connection_id=c.id
		LEFT JOIN workbench_connector_connections p ON p.id=c.connector_connection_id
		WHERE (g.connection_id IS NULL OR g.access_state='available')
		  AND (c.connector_connection_id IS NULL OR (c.access_state='available' AND p.auth_state='authorized'))
		ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []common.WorkbenchQueryScope
	for rows.Next() {
		var config, login string
		var version int64
		var source common.WorkbenchQueryScope
		if err := rows.Scan(&config, &version, &login, &source.AccountID, &source.AuthorizationGeneration); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(config), &source.WorkbenchConnection); err != nil {
			return nil, err
		}
		source.Version, source.AccountLogin = version, login
		result = append(result, source)
	}
	return result, rows.Err()
}

func workbenchQueryTasks(ctx context.Context, tx *sql.Tx) ([]common.WorkbenchTask, error) {
	rows, err := tx.QueryContext(ctx, "SELECT source_key,payload FROM workbench_objects WHERE "+workbenchObjectVisible+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []common.WorkbenchTask
	var ids []string
	for rows.Next() {
		task, err := scanWorkbench(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
		ids = append(ids, task.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	first := 0
	for chunk := range slices.Chunk(ids, 500) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		labels, err := workbenchTaskLabels(tx, chunk)
		if err != nil {
			return nil, err
		}
		for i, id := range chunk {
			tasks[first+i].WorkbenchLabels = labels[id]
		}
		first += len(chunk)
	}
	return tasks, nil
}
