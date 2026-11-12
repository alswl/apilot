package dal

import (
	"encoding/json"

	"github.com/alswl/apilot/pkg/common"
)

func (s *Store) Observers() ([]common.Observer, error) {
	rows, e := s.db.Query("SELECT payload,env_json FROM observers ORDER BY name")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []common.Observer{}
	for rows.Next() {
		var b, env string
		var v common.Observer
		if e = rows.Scan(&b, &env); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &v); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(env), &v.Env); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) SaveObserver(v common.Observer) error {
	_, e := s.db.Exec("INSERT INTO observers(id,name,payload,env_json) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,payload=excluded.payload,env_json=excluded.env_json", v.ID, v.Name, encode(v), encode(v.Env))
	return e
}

func (s *Store) DeleteObserver(id string) error {
	_, e := s.db.Exec("DELETE FROM observers WHERE id=?", id)
	return e
}
