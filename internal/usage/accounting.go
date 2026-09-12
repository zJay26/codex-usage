package usage

import (
	"context"
	"errors"
	"os"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/store"
)

type recordError struct{ error }

func (s *Scanner) scanFile(ctx context.Context, home, path string, meta model.SessionInfo, info os.FileInfo) (fileScanResult, error) {
	var result fileScanResult
	err := s.Store.Ingest(ctx, func(tx *store.Store) error {
		worker := &Scanner{Store: tx, MaxRelevantRecord: s.MaxRelevantRecord, Now: s.Now}
		var err error
		info, err = os.Stat(path)
		if err != nil {
			return err
		}
		result, err = worker.scanFileTransaction(ctx, home, path, meta, info)
		return err
	})
	if err != nil {
		return fileScanResult{}, err
	}
	return result, nil
}

// A context boundary alone is insufficient: legacy writers keep a session
// counter across turns. Exact total==last at a NEW turn proves a fresh counter
// even when it equals or exceeds the previous turn's total. A reset only proves
// the current boundary: the next turn may continue that same cumulative series
// or initially repeat its last snapshot. Never reset from the saved scope alone.
// The result marks a boundary where preserving the baseline is conservative,
// but missing snapshots prevent distinguishing a continuation from a new reset.
func selectCounterScope(cursor *store.FileCursor, info tokenInfo) (unverified bool) {
	current, last := info.Total.usage(), info.Last.usage()
	newTurn := cursor.TurnID != "" && cursor.Accounting.LastTurnID != "" && cursor.TurnID != cursor.Accounting.LastTurnID
	reset := newTurn && !current.IsZero() && !last.IsZero() && current.Equal(last)
	if reset {
		cursor.Accounting.Scope = "turn"
		cursor.Segment++
		cursor.Cumulative = model.TokenUsage{}
		cursor.LastEventID = ""
		cursor.InheritedBaseline = false
	} else if newTurn {
		continued := info.Total.withMissingSubsets(cursor.Cumulative)
		unverified = cursor.Accounting.Scope == "turn" && current.Total > cursor.Cumulative.Total &&
			continued.MonotonicFrom(cursor.Cumulative) && !continued.Sub(cursor.Cumulative).Equal(last)
		// Preserve the preceding baseline for a continuing series, a repeated
		// snapshot, or a boundary without enough evidence to prove a reset.
		// processRecord still handles an actual regression conservatively and
		// warns when last_token_usage cannot explain the whole reset.
		cursor.Accounting.Scope = "session"
	}
	cursor.Accounting.LastTurnID = cursor.TurnID
	return unverified
}

func isRecordError(err error) bool {
	var record *recordError
	return errors.As(err, &record)
}

func scopedEventID(sessionID string, cursor *store.FileCursor, current model.TokenUsage) string {
	if cursor.TurnID != "" {
		return stableJSONLEventID(sessionID+"\x00turn:"+cursor.TurnID, 0, current)
	}
	return stableJSONLEventID(sessionID, cursor.Segment, current)
}

// Copied/restored files need not contain an identical snapshot before their new
// tail. For a proven turn counter, the turn's existing ledger is its high-water
// mark. Check once per turn per file transaction, including when another writer
// has advanced this turn since this physical cursor last committed.
func (s *Scanner) inheritTurnProgress(ctx context.Context, cursor *store.FileCursor) error {
	if cursor.Accounting.Scope != "turn" || cursor.Accounting.LegacyHistory || cursor.TurnID == "" || cursor.SessionID == "" {
		return nil
	}
	if s.turnBaselines == nil {
		s.turnBaselines = map[string]bool{}
	}
	if s.turnBaselines[cursor.TurnID] {
		return nil
	}
	usage, err := s.Store.LegacyTurnUsage(ctx, cursor.SessionID, cursor.TurnID)
	if err != nil {
		return err
	}
	s.turnBaselines[cursor.TurnID] = true
	if usage.Total > cursor.Cumulative.Total {
		cursor.Cumulative = usage
		cursor.LastEventID = ""
		cursor.InheritedBaseline = true
	}
	return nil
}
