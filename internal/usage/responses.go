package usage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/store"
)

type responseUsageRecord struct {
	ThreadID   string      `json:"thread_id"`
	TurnID     string      `json:"turn_id"`
	ResponseID string      `json:"response_id"`
	Usage      tokenVector `json:"usage"`
	TurnUsage  tokenVector `json:"turn_token_usage"`
}

func (s *Scanner) processResponse(ctx context.Context, env envelope, path, home string, meta model.SessionInfo, cursor *store.FileCursor, result *fileScanResult) error {
	var record responseUsageRecord
	if err := json.Unmarshal(env.Payload, &record); err != nil {
		return &recordError{err}
	}
	// session_id is a runtime identity; thread_id proves ownership of the
	// persisted conversation, including copied parent records in fork files.
	if record.ThreadID != "" && record.ThreadID != cursor.SessionID {
		return nil
	}
	u, total := record.Usage.usage(), record.TurnUsage.usage()
	if record.ThreadID == "" || record.TurnID == "" || record.ResponseID == "" ||
		!validTokenUsage(u) || u.IsZero() || !validTokenUsage(total) || !total.MonotonicFrom(u) {
		return &recordError{fmt.Errorf("独立请求用量缺少身份、分类或有效 turn_token_usage")}
	}
	at, err := parseTimestamp(env.Timestamp)
	if err != nil {
		return &recordError{err}
	}
	if cursor.TurnID == "" {
		cursor.TurnID = record.TurnID
	}
	before, err := s.Store.TurnUsage(ctx, record.ThreadID, record.TurnID)
	if err != nil {
		return err
	}
	delta := total.Sub(before)
	if total.Total > before.Total && !validTokenUsage(delta) {
		return &recordError{fmt.Errorf("独立请求累计与已入账分类冲突: current=(%s) accounted=(%s)", total, before)}
	}
	inserted, err := s.Store.RecordResponse(ctx, record.ThreadID, record.TurnID, record.ResponseID, u, total)
	if err != nil {
		return err
	} // Roll back the file transaction on identity conflicts.
	if !inserted || total.Total <= before.Total {
		result.Duplicates++
		return nil
	}
	confidence := model.ConfidenceExact
	if !delta.Equal(u) {
		// A later cumulative record can recover a missing prefix, but its
		// original request timestamps/model boundaries are no longer known.
		confidence = model.ConfidenceGapFallback
		result.Warnings++
		if err := s.Store.AddWarning(ctx, "response_usage_gap", path,
			fmt.Sprintf("turn=%s：独立请求累计补足 %d tokens，当前请求为 %d；缺失明细的时间归属不确定", record.TurnID, delta.Total, u.Total)); err != nil {
			return err
		}
	}
	event := model.UsageEvent{
		ID:        fmt.Sprintf("jsonl-response:%x", sha256.Sum256([]byte(record.ThreadID+"\x00"+record.ResponseID))),
		SessionID: record.ThreadID, TurnID: record.TurnID, Timestamp: at, ObservedAt: s.Now(),
		Model: firstNonEmpty(cursor.Model, meta.Model), Source: firstNonEmpty(cursor.Source, meta.Source, meta.ThreadSource),
		AgentType: firstNonEmpty(cursor.AgentType, meta.AgentType, "main"), ProjectPath: firstNonEmpty(cursor.ProjectPath, meta.ProjectPath),
		ThreadTitle: meta.Title, Usage: delta, Provenance: model.ProvenanceSessionJSONL, Confidence: confidence, CodexHome: home,
	}
	if inserted, err := s.Store.InsertEvent(ctx, event, path); err != nil {
		return err
	} else if inserted {
		result.EventsInserted++
	} else {
		result.Duplicates++
	}
	return nil
}

// Keep the legacy cursor in sync even when detailed records own this turn. A
// later legacy-only turn may resume the same session-wide cumulative counter.
func (s *Scanner) observeLegacyResponseTurn(ctx context.Context, info tokenInfo, path string, cursor *store.FileCursor, result *fileScanResult) error {
	last := info.Last.usage()
	if validTokenUsage(last) && !last.IsZero() {
		known, err := s.Store.ResponseUsageKnown(ctx, cursor.SessionID, cursor.TurnID, last)
		if err != nil {
			return err
		}
		if !known {
			result.Warnings++
			if err := s.Store.AddWarning(ctx, "response_usage_unmatched", path,
				fmt.Sprintf("turn=%s：旧通知含未匹配的请求用量；保留独立请求累计，可能缺少尾部明细", cursor.TurnID)); err != nil {
				return err
			}
		}
	}
	cursor.Cumulative = info.Total.withMissingSubsets(cursor.Cumulative)
	cursor.LastEventID = ""
	result.Duplicates++
	return s.Store.PutSessionProgress(ctx, cursor.SessionID, cursor.Segment, cursor.Cumulative, cursor.Accounting)
}
