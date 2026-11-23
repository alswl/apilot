package dal

import (
	"context"
	"path/filepath"
	"testing"
)

func TestChangeReaderOnlyObservesCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	read, closeReader, err := OpenChangeReader(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeReader()
	version := func() int64 {
		t.Helper()
		v, err := read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	first := version()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO settings (id,payload) VALUES (1,'{}')"); err != nil {
		t.Fatal(err)
	}
	if got := version(); got != first {
		t.Fatalf("observed uncommitted write: %d -> %d", first, got)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := version(); got != first {
		t.Fatalf("observed rolled back write: %d -> %d", first, got)
	}
	if _, err := store.db.Exec("INSERT INTO settings (id,payload) VALUES (1,'{}')"); err != nil {
		t.Fatal(err)
	}
	if got := version(); got == first {
		t.Fatal("committed write did not change observer version")
	}
}
