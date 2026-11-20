package dal

import (
	"database/sql"
	"errors"

	"github.com/alswl/apilot/pkg/common"
)

func (s *Store) WorkbenchBridge(id string) (common.WorkbenchBridgeInput, error) {
	var bridge common.WorkbenchBridgeInput
	err := s.db.QueryRow("SELECT endpoint,token,certificate FROM workbench_obsidian_bridges WHERE connection_id=?", id).
		Scan(&bridge.Endpoint, &bridge.Token, &bridge.Certificate)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return bridge, err
}
