package usage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/store"
)

func (s *Scanner) backfillRelationship(ctx context.Context, path string, cursor store.FileCursor) error {
	checked, err := s.Store.RelationshipChecked(ctx, path)
	if err != nil || checked || cursor.SessionID == "" {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := bufio.NewReaderSize(io.LimitReader(f, cursor.Offset), 64<<10)
	for {
		record, n, complete, large, err := readSelectiveRecord(reader, s.MaxRelevantRecord)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if n == 0 || !complete {
			return nil
		}
		if large {
			continue
		}
		var env envelope
		var meta sessionMetaPayload
		if json.Unmarshal(record, &env) != nil || env.Type != "session_meta" {
			continue
		}
		if json.Unmarshal(env.Payload, &meta) != nil {
			return nil
		}
		// The first metadata owns the physical file, including copied fork prefixes.
		if firstNonEmpty(meta.ID, meta.SessionID) != cursor.SessionID {
			return nil
		}
		if err := s.Store.PutRelationship(ctx, cursor.SessionID, model.SpawnParent(meta.Source), meta.ForkedFromID); err != nil {
			return err
		}
		return s.Store.MarkRelationshipChecked(ctx, path)
	}
}
