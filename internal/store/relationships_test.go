package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestReadStateRelationshipsUsesExplicitUnambiguousEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE threads(id TEXT PRIMARY KEY, rollout_path TEXT, source TEXT)`,
		`INSERT INTO threads VALUES('metadata','a.jsonl','{"subagent":{"thread_spawn":{"parent_thread_id":"explicit"}}}'),('edge','b.jsonl','cli'),('conflict','c.jsonl','cli'),('plain','d.jsonl','cli')`,
		`CREATE TABLE thread_spawn_edges(parent_thread_id TEXT, child_thread_id TEXT, status TEXT)`,
		`INSERT INTO thread_spawn_edges VALUES('fallback','metadata','complete'),('parent','edge','complete'),('a','conflict','complete'),('b','conflict','complete')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	read := func() map[string]string {
		t.Helper()
		items, err := ReadStateThreads(context.Background(), path, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		parents := map[string]string{}
		for _, item := range items {
			parents[item.SessionID] = item.ParentSessionID
		}
		return parents
	}
	parents := read()
	if parents["metadata"] != "explicit" || parents["edge"] != "parent" || parents["conflict"] != "" || parents["plain"] != "" {
		t.Fatalf("unexpected inferred relationships: %v", parents)
	}
	if _, err := db.Exec(`DROP TABLE thread_spawn_edges`); err != nil {
		t.Fatal(err)
	}
	parents = read()
	if parents["metadata"] != "explicit" || parents["edge"] != "" {
		t.Fatalf("legacy state schema fallback failed: %v", parents)
	}
}
