package usage

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/store"
)

// One metadata-only pass also recovers explicit fields ignored by older versions.
// The existing fork inspector supplies the physical child boundary.
func (s *Scanner) backfillJSONLModes(ctx context.Context, home, path string, cursor store.FileCursor) error {
	if cursor.Offset <= 0 || s.Store.ModeFileChecked(ctx, path) {
		return nil
	}
	start := int64(0)
	if cursor.ForkedFromID != "" {
		inspection, err := s.inspectRollout(ctx, path)
		if err != nil {
			return err
		}
		if inspection.ReplayOffset <= 0 {
			return nil
		}
		start = inspection.ReplayOffset
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(io.LimitReader(f, cursor.Offset-start), 64<<10)
	items := []store.TurnMode{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, n, complete, large, err := readSelectiveRecord(reader, s.MaxRelevantRecord)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if n == 0 {
			break
		}
		if !complete || large {
			continue
		}
		var env envelope
		if json.Unmarshal(record, &env) != nil || env.Type != "turn_context" {
			continue
		}
		var p turnContextPayload
		if json.Unmarshal(env.Payload, &p) != nil || p.TurnID == "" {
			continue
		}
		var tier string
		if len(p.ServiceTier) == 0 || string(p.ServiceTier) == "null" || json.Unmarshal(p.ServiceTier, &tier) != nil {
			continue
		}
		items = append(items, store.TurnMode{Home: home, SessionID: cursor.SessionID, TurnID: p.TurnID, Mode: model.ModeFromTier(tier, "jsonl_turn_context")})
		if len(items) >= 128 {
			if err := s.Store.PutTurnModes(ctx, items); err != nil {
				return err
			}
			items = nil
		}
	}
	if err := s.Store.PutTurnModes(ctx, items); err != nil {
		return err
	}
	return s.Store.MarkModeFileChecked(ctx, path)
}

type debugToken struct {
	text   string
	quoted bool
}

// Lex Debug output before looking up fields: user text may contain entire fake submissions.
func debugTokens(body string) ([]debugToken, bool) {
	var out []debugToken
	for i := 0; i < len(body); {
		c := body[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			i++
			continue
		}
		if c == '"' {
			start := i
			i++
			closed := false
			for i < len(body) {
				if body[i] == '\\' {
					i += 2
					continue
				}
				if body[i] == '"' {
					i++
					closed = true
					break
				}
				i++
			}
			if !closed || i > len(body) {
				return nil, false
			}
			// Long prompt strings need only a boundary, never their contents.
			value := ""
			if i-start <= 256 {
				value, _ = strconv.Unquote(body[start:i])
			}
			out = append(out, debugToken{value, true})
			continue
		}
		if strings.ContainsRune("{}[]():,=", rune(c)) {
			out = append(out, debugToken{string(c), false})
			i++
			continue
		}
		start := i
		for i < len(body) && !strings.ContainsRune(" \n\r\t{}[]():,=\"", rune(body[i])) {
			i++
		}
		out = append(out, debugToken{body[start:i], false})
	}
	return out, true
}

func debugField(tokens []debugToken, name string) []debugToken {
	// Value is either { ... } or Type { ... }; fields must be direct members.
	start := 0
	for start < len(tokens) && tokens[start].text != "{" {
		start++
	}
	start++
	depth := 0
	for i := start; i+1 < len(tokens); i++ {
		t := tokens[i]
		if t.quoted {
			continue
		}
		if depth == 0 && t.text == "}" {
			break
		}
		if depth == 0 && t.text == name && tokens[i+1].text == ":" {
			j := i + 2
			d := 0
			for ; j < len(tokens); j++ {
				v := tokens[j]
				if v.quoted {
					continue
				}
				if d == 0 && (v.text == "," || v.text == "}") {
					break
				}
				if strings.Contains("{[(", v.text) {
					d++
				}
				if strings.Contains("}])", v.text) {
					d--
				}
			}
			return tokens[i+2 : j]
		}
		if strings.Contains("{[(", t.text) {
			depth++
		}
		if strings.Contains("}])", t.text) {
			depth--
		}
	}
	return nil
}

func debugTier(v []debugToken) (model.ServiceMode, bool) {
	parts := []string{}
	for _, t := range v {
		if t.quoted {
			parts = append(parts, strconv.Quote(t.text))
		} else {
			parts = append(parts, t.text)
		}
	}
	s := strings.Join(parts, "")
	if s == "None" || s == "" {
		return model.ServiceMode{}, false
	}
	if s == "Some(None)" {
		return model.ModeFromTier("default", "diagnostic_turn_input"), true
	}
	for _, prefix := range []string{"Some(Some(", "Some("} {
		suffix := ")"
		if prefix == "Some(Some(" {
			suffix = "))"
		}
		if strings.HasPrefix(s, prefix) && strings.HasSuffix(s, suffix) {
			tier, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(s, prefix), suffix))
			if err == nil {
				return model.ModeFromTier(tier, "diagnostic_turn_input"), true
			}
		}
	}
	return model.ServiceMode{}, false
}

func parseDiagnosticMode(body string) (string, model.ServiceMode, bool) {
	tokens, ok := debugTokens(body)
	if !ok {
		return "", model.ServiceMode{}, false
	}
	for i := 0; i+4 < len(tokens); i++ {
		if tokens[i].quoted || tokens[i].text != "Submission" || tokens[i+1].text != "sub" || tokens[i+2].text != "=" || tokens[i+3].text != "Submission" || tokens[i+4].text != "{" {
			continue
		}
		sub := tokens[i+3:]
		id := debugField(sub, "id")
		op := debugField(sub, "op")
		if len(id) != 1 || !id[0].quoted || id[0].text == "" || len(op) == 0 || op[0].text != "TurnInput" {
			return "", model.ServiceMode{}, false
		}
		request := debugField(op, "request")
		if len(request) == 0 || request[0].text != "TurnInputRequest" {
			return "", model.ServiceMode{}, false
		}
		settings := debugField(request, "thread_settings")
		mode, found := debugTier(debugField(settings, "service_tier"))
		// Accept a direct start setting, but do not guess precedence if two
		// explicit fields in this request disagree about the mode.
		if startMode, startFound := debugTier(debugField(debugField(request, "start"), "service_tier")); startFound {
			if found && mode.ServiceMode != startMode.ServiceMode {
				return id[0].text, model.ServiceMode{ServiceMode: model.ModeUnknown, ModeSource: "diagnostic_turn_input_conflict", ModeAssumed: true}, true
			}
			mode, found = startMode, true
		}
		return id[0].text, mode, found
	}
	return "", model.ServiceMode{}, false
}

// Run after JSONL ingestion, in the existing background scan. Each bounded batch
// commits only mode metadata; SQLite readers continue serving the dashboard.
func (s *Scanner) scanDiagnosticModes(ctx context.Context, home string) {
	paths, _ := filepath.Glob(filepath.Join(home, "logs_*.sqlite"))
	for _, path := range paths {
		progress, err := s.Store.DiagnosticCursor(ctx, path)
		if err != nil {
			continue
		}
		if err = s.scanDiagnosticFile(ctx, home, path, &progress); err != nil {
			progress.State = "unavailable"
		}
		_ = s.Store.PutDiagnosticCursor(ctx, progress)
	}
}

func (s *Scanner) scanDiagnosticFile(ctx context.Context, home, path string, p *store.DiagnosticProgress) error {
	db, err := store.OpenDiagnostics(path)
	if err != nil {
		return err
	}
	defer db.Close()
	var first, last int64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MIN(id),0),COALESCE(MAX(id),0) FROM logs`).Scan(&first, &last); err != nil {
		return err
	}
	if last < p.LastID {
		p.LastID, p.LastTS = 0, 0
	}
	if p.LastID > 0 && p.LastTS > 0 {
		var stamp int64
		err := db.QueryRowContext(ctx, `SELECT ts FROM logs WHERE id=?`, p.LastID).Scan(&stamp)
		if err == nil && stamp != p.LastTS {
			p.LastID, p.LastTS = 0, 0
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if p.LastID < first-1 {
		p.LastID = first - 1
	}
	p.TargetID = last
	p.State = "pending"
	if err := s.Store.PutDiagnosticCursor(ctx, *p); err != nil {
		return err
	}
	for p.LastID < last {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(p.LastID+20000, last)
		rows, err := db.QueryContext(ctx, `SELECT thread_id,feedback_log_body FROM logs WHERE id>? AND id<=?
			AND target='codex_core::session::handlers' AND thread_id IS NOT NULL
			AND length(feedback_log_body)<=8388608`, p.LastID, end)
		if err != nil {
			return err
		}
		items := []store.TurnMode{}
		for rows.Next() {
			var thread, body string
			if err := rows.Scan(&thread, &body); err != nil {
				rows.Close()
				return err
			}
			if turn, mode, ok := parseDiagnosticMode(body); ok {
				items = append(items, store.TurnMode{Home: home, SessionID: thread, TurnID: turn, Mode: mode})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if err := s.Store.PutTurnModes(ctx, items); err != nil {
			return err
		}
		p.LastID = end
		p.LastTS = 0
		err = db.QueryRowContext(ctx, `SELECT ts FROM logs WHERE id=?`, end).Scan(&p.LastTS)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := s.Store.PutDiagnosticCursor(ctx, *p); err != nil {
			return err
		}
	}
	p.State = "complete"
	return nil
}
