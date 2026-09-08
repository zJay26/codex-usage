package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/zJay26/codex-usage/internal/timezone"
)

func (s *Store) Location() *time.Location {
	if s.location != nil {
		return s.location
	}
	return time.Local
}

func hourStart(t time.Time) time.Time {
	return t.Add(-time.Duration(t.Minute())*time.Minute - time.Duration(t.Second())*time.Second - time.Duration(t.Nanosecond()))
}

func (s *Store) initTimezone(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name string
	err = tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='accounting_timezone'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		name = os.Getenv("CODEX_USAGE_TIMEZONE")
		if name == "" {
			name = timezone.Detect()
		}
		if _, err = time.LoadLocation(name); err != nil {
			return fmt.Errorf("invalid accounting timezone: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO meta VALUES('accounting_timezone',?)`, name); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	s.location, err = time.LoadLocation(name)
	if err != nil {
		return err
	}
	// Add UTC hour identities without rewriting historical token amounts or the
	// original recorded date labels. Runs only for rows predating this column.
	rows, err := tx.QueryContext(ctx, `SELECT id,usage_at FROM usage_events WHERE usage_at>0 AND hour_start=0`)
	if err != nil {
		return err
	}
	type oldHour struct {
		id string
		at int64
	}
	var pending []oldHour
	var commonOffset int
	uniformOffset := true
	for rows.Next() {
		var row oldHour
		if err := rows.Scan(&row.id, &row.at); err != nil {
			rows.Close()
			return err
		}
		_, offset := time.Unix(row.at, 0).In(s.location).Zone()
		offset = (offset%3600 + 3600) % 3600
		if len(pending) == 0 {
			commonOffset = offset
		} else if offset != commonOffset {
			uniformOffset = false
		}
		pending = append(pending, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(pending) > 0 && uniformOffset {
		// Integer-hour DST changes share the same UTC rounding. Most ledgers can
		// backfill in one statement instead of parsing an UPDATE for every event.
		_, err = tx.ExecContext(ctx, `UPDATE usage_events SET hour_start=usage_at-((usage_at+?)%3600) WHERE usage_at>0 AND hour_start=0`, commonOffset)
		if err != nil {
			return err
		}
	} else if len(pending) > 0 {
		statement, err := tx.PrepareContext(ctx, `UPDATE usage_events SET hour_start=? WHERE id=?`)
		if err != nil {
			return err
		}
		defer statement.Close()
		for _, row := range pending {
			if _, err = statement.ExecContext(ctx, hourStart(time.Unix(row.at, 0).In(s.location)).Unix(), row.id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
