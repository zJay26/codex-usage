package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/zJay26/codex-usage/internal/model"
)

func migrateRelationships(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		`ALTER TABLE sessions ADD COLUMN parent_session_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN forked_from_id TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS relationship_backfills(path TEXT PRIMARY KEY)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_parent ON sessions(parent_session_id)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}

func (s *Store) PutRelationship(ctx context.Context, id, parent, fork string) error {
	if id == "" || (parent == "" && fork == "") {
		return nil
	}
	_, err := s.writer().ExecContext(ctx, `UPDATE sessions SET
		parent_session_id=CASE WHEN ?<>'' THEN ? ELSE parent_session_id END,
		forked_from_id=CASE WHEN ?<>'' THEN ? ELSE forked_from_id END
		WHERE session_id=? AND ((?<>'' AND parent_session_id<>?) OR (?<>'' AND forked_from_id<>?))`, parent, parent, fork, fork, id, parent, parent, fork, fork)
	return err
}

func (s *Store) RelationshipChecked(ctx context.Context, path string) (bool, error) {
	var count int
	err := s.reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM relationship_backfills WHERE path=?`, path).Scan(&count)
	return count > 0, err
}
func (s *Store) MarkRelationshipChecked(ctx context.Context, path string) error {
	_, err := s.writer().ExecContext(ctx, `INSERT OR IGNORE INTO relationship_backfills VALUES(?)`, path)
	return err
}

func (s *Store) SessionRelationships(ctx context.Context) (map[string]model.SessionInfo, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT session_id,title,parent_session_id,forked_from_id FROM sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]model.SessionInfo{}
	for rows.Next() {
		var item model.SessionInfo
		if err := rows.Scan(&item.SessionID, &item.Title, &item.ParentSessionID, &item.ForkedFromID); err != nil {
			return nil, err
		}
		out[item.SessionID] = item
	}
	return out, rows.Err()
}

func readSpawnEdges(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(thread_spawn_edges)`)
	if err != nil {
		return nil, err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var value any
		if err := rows.Scan(&cid, &name, &typ, &nn, &value, &pk); err != nil {
			rows.Close()
			return nil, err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	out := map[string]string{}
	if !columns["child_thread_id"] || !columns["parent_thread_id"] {
		return out, nil
	}
	edges, err := db.QueryContext(ctx, `SELECT child_thread_id,MIN(parent_thread_id) FROM thread_spawn_edges WHERE parent_thread_id<>'' GROUP BY child_thread_id HAVING COUNT(DISTINCT parent_thread_id)=1`)
	if err != nil {
		return nil, err
	}
	defer edges.Close()
	for edges.Next() {
		var child, parent string
		if err := edges.Scan(&child, &parent); err != nil {
			return nil, err
		}
		out[child] = parent
	}
	return out, edges.Err()
}
