package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zJay26/codex-usage/internal/model"
)

var modeUsageSQL = func() string {
	var columns []string
	for _, mode := range []string{"fast", "unknown"} {
		for _, column := range []string{"input_tokens", "cached_input_tokens", "cache_write_input_tokens", "output_tokens", "reasoning_output_tokens", "total_tokens"} {
			columns = append(columns, "COALESCE(SUM(CASE WHEN e.service_mode='"+mode+"' THEN e."+column+" ELSE 0 END),0)")
		}
	}
	return strings.Join(columns, ",")
}()

func modeUsageDest(m *model.ModeUsage) []any {
	return []any{&m.Fast.Input, &m.Fast.CachedInput, &m.Fast.CacheWriteInput, &m.Fast.Output, &m.Fast.ReasoningOutput, &m.Fast.Total,
		&m.Unknown.Input, &m.Unknown.CachedInput, &m.Unknown.CacheWriteInput, &m.Unknown.Output, &m.Unknown.ReasoningOutput, &m.Unknown.Total}
}

func migrateServiceModes(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS mode_file_backfills (path TEXT PRIMARY KEY)`,
		`ALTER TABLE usage_events ADD COLUMN service_mode TEXT NOT NULL DEFAULT 'unknown'`,
		`ALTER TABLE usage_events ADD COLUMN service_tier TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE usage_events ADD COLUMN mode_source TEXT NOT NULL DEFAULT 'unavailable'`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_events_turn_mode ON usage_events(session_id,turn_id,codex_home)`,
		`CREATE TABLE IF NOT EXISTS turn_service_modes (
			codex_home TEXT NOT NULL, session_id TEXT NOT NULL, turn_id TEXT NOT NULL,
			service_mode TEXT NOT NULL, service_tier TEXT NOT NULL, mode_source TEXT NOT NULL,
			PRIMARY KEY(codex_home,session_id,turn_id))`,
		`CREATE TABLE IF NOT EXISTS diagnostic_cursors (
			path TEXT PRIMARY KEY, last_id INTEGER NOT NULL DEFAULT 0, last_ts INTEGER NOT NULL DEFAULT 0,
			target_id INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL DEFAULT 'pending')`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ModeFileChecked(ctx context.Context, path string) bool {
	var count int
	return s.reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM mode_file_backfills WHERE path=?`, path).Scan(&count) == nil && count > 0
}
func (s *Store) MarkModeFileChecked(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO mode_file_backfills VALUES(?)`, path)
	return err
}

func modeHomeKey(home string) string {
	home = filepath.Clean(home)
	if runtime.GOOS == "windows" {
		home = strings.ToLower(strings.TrimPrefix(home, `\\?\`))
	}
	return home
}

type TurnMode struct {
	Home, SessionID, TurnID string
	Mode                    model.ServiceMode
}

func (s *Store) ModeForTurn(ctx context.Context, home, session, turn string) (model.ServiceMode, error) {
	m := model.ServiceMode{}
	err := s.reader().QueryRowContext(ctx, `SELECT service_mode,service_tier,mode_source FROM turn_service_modes
		WHERE codex_home=? AND session_id=? AND turn_id=?`, modeHomeKey(home), session, turn).Scan(&m.ServiceMode, &m.ServiceTier, &m.ModeSource)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return m.Normalized(), err
}

func modeRank(source string) int {
	if strings.HasPrefix(source, "jsonl_turn_context") {
		return 2
	}
	if strings.HasPrefix(source, "diagnostic_turn_input") {
		return 1
	}
	return 0
}

// Metadata-only transaction: no token, ownership, timestamp or dedupe fields change.
func (s *Store) PutTurnModes(ctx context.Context, items []TurnMode) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var changed int64
	for _, item := range items {
		if item.SessionID == "" || item.TurnID == "" {
			continue
		}
		key := modeHomeKey(item.Home)
		m := item.Mode.Normalized()
		var old model.ServiceMode
		err := tx.QueryRowContext(ctx, `SELECT service_mode,service_tier,mode_source FROM turn_service_modes
			WHERE codex_home=? AND session_id=? AND turn_id=?`, key, item.SessionID, item.TurnID).Scan(&old.ServiceMode, &old.ServiceTier, &old.ModeSource)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if modeRank(old.ModeSource) > modeRank(m.ModeSource) {
				continue
			}
			if modeRank(old.ModeSource) == modeRank(m.ModeSource) {
				if strings.HasSuffix(old.ModeSource, "_conflict") {
					continue
				}
				if old.ServiceMode != m.ServiceMode {
					m.ServiceMode, m.ServiceTier, m.ModeSource = model.ModeUnknown, "", m.ModeSource+"_conflict"
				} else if old.ServiceTier == m.ServiceTier && old.ModeSource == m.ModeSource {
					continue
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO turn_service_modes VALUES(?,?,?,?,?,?)
			ON CONFLICT(codex_home,session_id,turn_id) DO UPDATE SET service_mode=excluded.service_mode,
			service_tier=excluded.service_tier,mode_source=excluded.mode_source`, key, item.SessionID, item.TurnID, m.ServiceMode, m.ServiceTier, m.ModeSource); err != nil {
			return err
		}
		homeExpr := "codex_home"
		if runtime.GOOS == "windows" {
			homeExpr = "lower(codex_home)"
		}
		result, err := tx.ExecContext(ctx, `UPDATE usage_events SET service_mode=?,service_tier=?,mode_source=?
			WHERE session_id=? AND turn_id=? AND `+homeExpr+`=?`, m.ServiceMode, m.ServiceTier, m.ModeSource, item.SessionID, item.TurnID, key)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		changed += n
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if changed > 0 {
		s.revision.Add(1)
	}
	return nil
}

type DiagnosticProgress struct {
	Path     string `json:"path"`
	LastID   int64  `json:"last_id"`
	LastTS   int64  `json:"-"`
	TargetID int64  `json:"target_id"`
	State    string `json:"state"`
}

func (s *Store) DiagnosticCursor(ctx context.Context, path string) (DiagnosticProgress, error) {
	p := DiagnosticProgress{Path: path, State: "pending"}
	err := s.reader().QueryRowContext(ctx, `SELECT last_id,last_ts,target_id,state FROM diagnostic_cursors WHERE path=?`, path).Scan(&p.LastID, &p.LastTS, &p.TargetID, &p.State)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return p, err
}

func (s *Store) PutDiagnosticCursor(ctx context.Context, p DiagnosticProgress) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO diagnostic_cursors VALUES(?,?,?,?,?) ON CONFLICT(path)
		DO UPDATE SET last_id=excluded.last_id,last_ts=excluded.last_ts,target_id=excluded.target_id,state=excluded.state`, p.Path, p.LastID, p.LastTS, p.TargetID, p.State)
	return err
}

func (s *Store) DiagnosticProgress(ctx context.Context) ([]DiagnosticProgress, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT path,last_id,last_ts,target_id,state FROM diagnostic_cursors ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiagnosticProgress{}
	for rows.Next() {
		var p DiagnosticProgress
		if err := rows.Scan(&p.Path, &p.LastID, &p.LastTS, &p.TargetID, &p.State); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func OpenDiagnostics(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", sqliteURI(path, "mode=ro&_pragma=busy_timeout(500)&_pragma=query_only(1)"))
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}
