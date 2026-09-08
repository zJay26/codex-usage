package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/zJay26/codex-usage/internal/model"
)

type database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) writer() database {
	if s.tx != nil {
		return s.tx
	}
	return s.db
}

// beginWrite reuses the file transaction when an operation has several writes.
func (s *Store) beginWrite(ctx context.Context) (database, func() error, func(), error) {
	if s.tx != nil {
		return s.tx, func() error { return nil }, func() {}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	return tx, tx.Commit, func() { _ = tx.Rollback() }, nil
}

// Ingest serializes cursor reads across processes with BEGIN IMMEDIATE. Events,
// classification corrections, turn modes, progress and file offsets commit as
// one unit. A failed file can be retried without losing or duplicating usage.
func (s *Store) Ingest(ctx context.Context, fn func(*Store) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	bound := &Store{db: s.db, tx: tx, path: s.path, machine: s.machine, location: s.location}
	if err = fn(bound); err != nil {
		return err
	}
	return tx.Commit()
}

// ReadSnapshot keeps rows, costs and their revision in the same WAL snapshot.
func (s *Store) ReadSnapshot(ctx context.Context, fn func(*Store) error) error {
	tx, err := s.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return fn(&Store{db: s.db, tx: tx, path: s.path, machine: s.machine, location: s.location})
}

func (s *Store) DataRevision(ctx context.Context) (uint64, error) {
	var revision uint64
	err := s.reader().QueryRowContext(ctx, `SELECT value FROM meta WHERE key='data_revision'`).Scan(&revision)
	return revision, err
}

// Revision is retained for internal callers that do not cache on failure.
func (s *Store) Revision() uint64 {
	revision, _ := s.DataRevision(context.Background())
	return revision
}

func (s *Store) SessionAccounting(ctx context.Context, sessionID string) (AccountingState, error) {
	var raw string
	err := s.reader().QueryRowContext(ctx, `SELECT accounting_json FROM session_cursors WHERE session_id=?`, sessionID).Scan(&raw)
	if err == sql.ErrNoRows {
		return AccountingState{}, nil
	}
	if err != nil {
		return AccountingState{}, err
	}
	var state AccountingState
	err = json.Unmarshal([]byte(raw), &state)
	return state, err
}

func (s *Store) TurnUsage(ctx context.Context, sessionID, turnID string) (model.TokenUsage, error) {
	var usage model.TokenUsage
	err := s.reader().QueryRowContext(ctx, `SELECT COALESCE(SUM(input_tokens),0),COALESCE(SUM(cached_input_tokens),0),
		COALESCE(SUM(cache_write_input_tokens),0),COALESCE(SUM(output_tokens),0),COALESCE(SUM(reasoning_output_tokens),0),COALESCE(SUM(total_tokens),0)
		FROM usage_events WHERE provenance='session_jsonl' AND session_id=? AND turn_id=?`, sessionID, turnID).Scan(
		&usage.Input, &usage.CachedInput, &usage.CacheWriteInput, &usage.Output, &usage.ReasoningOutput, &usage.Total)
	return usage, err
}

func migrateAccounting(ctx context.Context, tx *sql.Tx, version int) error {
	if version < 9 {
		for _, stmt := range []string{
			`ALTER TABLE usage_events ADD COLUMN hour_start INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE file_cursors ADD COLUMN accounting_json TEXT NOT NULL DEFAULT '{}'`,
			`ALTER TABLE session_cursors ADD COLUMN accounting_json TEXT NOT NULL DEFAULT '{}'`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return err
			}
		}
		// Preserve existing history. Only newly read records use the new parser;
		// an explicit scan --rebuild can recalculate retained source files.
		if version > 0 {
			for _, table := range []string{"file_cursors", "session_cursors"} {
				if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET accounting_json='{"legacy_history":true}'`); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO meta VALUES('accounting_upgrade_note','v2.6.0 preserves existing history; scan --rebuild explicitly recalculates retained JSONL sources')`); err != nil {
				return err
			}
		}
	}
	if version == 9 {
		var hasHistory bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM usage_events WHERE provenance='session_jsonl')`).Scan(&hasHistory); err != nil {
			return err
		}
		if hasHistory {
			// v2.6.0-v2.6.2 could repeat an entire cumulative baseline after a
			// turn change. New parser code cannot correct those existing deltas.
			// Keep the ledger and cursors until an explicit rebuild is requested,
			// and expose the issue before any background scan has run.
			reason := "v2.6.0–v2.6.2 可能在新 Turn 重复累计历史用量。现有统计已保留；请先备份并核对源 JSONL，再确认重建以修正。已删除源文件的历史无法通过重建恢复。"
			if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?)
				ON CONFLICT(key) DO UPDATE SET value=excluded.value`, historicalRebuildReasonKey, reason); err != nil {
				return err
			}
			if err := (&Store{tx: tx}).AddWarning(ctx, "schema_upgrade_rebuild", "", reason); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO meta VALUES('data_revision','1')`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_events_provenance_session_turn ON usage_events(provenance,session_id,turn_id)`); err != nil {
		return err
	}
	for _, table := range []string{"usage_events", "sessions"} {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			if _, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS revision_`+table+`_`+op+` AFTER `+op+` ON `+table+` BEGIN UPDATE meta SET value=CAST(value AS INTEGER)+1 WHERE key='data_revision'; END`); err != nil {
				return err
			}
		}
	}
	return nil
}
