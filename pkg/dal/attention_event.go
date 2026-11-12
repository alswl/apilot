package dal

import (
	"encoding/json"
	"sort"

	"github.com/alswl/apilot/pkg/common"
)

func (s *Store) SaveEvent(v common.AttentionEvent) error {
	_, e := s.db.Exec("INSERT INTO attention_events(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", v.ID, encode(v))
	return e
}

func (s *Store) Events() ([]common.AttentionEvent, error) {
	rows, e := s.db.Query("SELECT payload FROM attention_events ORDER BY id DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []common.AttentionEvent{}
	for rows.Next() {
		var b string
		var v common.AttentionEvent
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, rows.Err()
}

func (s *Store) DeleteEvent(id string) error {
	_, e := s.db.Exec("DELETE FROM attention_events WHERE id=?", id)
	return e
}
