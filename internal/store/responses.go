package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/zJay26/codex-usage/internal/model"
)

// Response records persist identity independently of the ledger: an overlapping
// record may confirm already-ingested legacy usage without inserting an event.
func (s *Store) RecordResponse(ctx context.Context, session, turn, response string, usage, cumulative model.TokenUsage) (bool, error) {
	u, _ := json.Marshal(usage)
	c, _ := json.Marshal(cumulative)
	var oldTurn, oldUsage, oldCumulative string
	err := s.writer().QueryRowContext(ctx, `SELECT turn_id,usage_json,cumulative_json FROM response_records WHERE session_id=? AND response_id=?`, session, response).Scan(&oldTurn, &oldUsage, &oldCumulative)
	if err == nil {
		if oldTurn != turn || oldUsage != string(u) || oldCumulative != string(c) {
			return false, fmt.Errorf("response_id %q has conflicting usage", response)
		}
		return false, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	_, err = s.writer().ExecContext(ctx, `INSERT INTO response_records(session_id,turn_id,response_id,usage_json,cumulative_json) VALUES(?,?,?,?,?)`, session, turn, response, string(u), string(c))
	return err == nil, err
}

func (s *Store) ResponseTurn(ctx context.Context, session, turn string) (bool, error) {
	var found bool
	err := s.reader().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM response_records WHERE session_id=? AND turn_id=?)`, session, turn).Scan(&found)
	return found, err
}

func (s *Store) ResponseUsageKnown(ctx context.Context, session, turn string, usage model.TokenUsage) (bool, error) {
	raw, _ := json.Marshal(usage)
	var found bool
	err := s.reader().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM response_records WHERE session_id=? AND turn_id=? AND usage_json=?)`, session, turn, string(raw)).Scan(&found)
	return found, err
}

func migrateResponses(ctx context.Context, tx *sql.Tx, version int) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS response_records (session_id TEXT NOT NULL, turn_id TEXT NOT NULL, response_id TEXT NOT NULL, usage_json TEXT NOT NULL, cumulative_json TEXT NOT NULL, PRIMARY KEY(session_id,response_id))`,
		`CREATE INDEX IF NOT EXISTS idx_response_turn ON response_records(session_id,turn_id)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if version > 0 && version < 11 {
		var history bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM usage_events WHERE provenance='session_jsonl')`).Scan(&history); err != nil {
			return err
		}
		if history {
			reason := "v2.6.4 开始读取独立请求用量，修复压缩请求漏计。现有统计已保留；请先备份并核对源 JSONL，再确认重建。已删除源文件的历史无法通过重建恢复。"
			if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=meta.value || ' ' || excluded.value`, historicalRebuildReasonKey, reason); err != nil {
				return err
			}
			if err := (&Store{tx: tx}).AddWarning(ctx, "schema_upgrade_rebuild", "", reason); err != nil {
				return err
			}
		}
	}
	return nil
}
