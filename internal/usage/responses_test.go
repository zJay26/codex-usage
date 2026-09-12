package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/store"
)

func responseLine(session, turn, id, at, usage, cumulative string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"token_usage_record","payload":{"thread_id":%q,"session_id":"runtime-id","turn_id":%q,"response_id":%q,"usage":%s,"turn_token_usage":%s}}`+"\n", at, session, turn, id, usage, cumulative)
}
func responseN(session, turn, id string, n, cumulative int64) string {
	return responseLine(session, turn, id, "2026-09-08T01:00:00Z", usage(n, 0, 0, 0, 0, n), usage(cumulative, 0, 0, 0, 0, cumulative))
}
func scanResponses(t *testing.T, st *store.Store, home string) ScanResult {
	t.Helper()
	r, err := (&Scanner{Store: st}).Scan(context.Background(), []string{home}, false)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCompactionResponseRealVectorsAcrossRestart(t *testing.T) {
	for _, incremental := range []bool{false, true} {
		t.Run(fmt.Sprint(incremental), func(t *testing.T) {
			st, home, path := accountingFixture(t)
			// These are numeric vectors from the audited four-request turn. No
			// conversation text or real identities are included in the fixture.
			reqs := []string{usage(224761, 19072, 0, 598, 0, 225359), usage(230167, 224640, 0, 4571, 0, 234738), usage(36108, 19072, 0, 446, 0, 36554), usage(37271, 35968, 0, 610, 137, 37881)}
			totals := []string{reqs[0], usage(454928, 243712, 0, 5169, 0, 460097), usage(491036, 262784, 0, 5615, 0, 496651), usage(528307, 298752, 0, 6225, 137, 534532)}
			old := []string{usage(10567493, 10153344, 0, 89642, 38260, 10657135), usage(10567493, 10153344, 0, 89642, 38260, 10657135), usage(10603601, 10172416, 0, 90088, 38260, 10693689), usage(10640872, 10208384, 0, 90698, 38397, 10731570)}
			appendAccounting(t, path, accountingMeta+accountingTurn("one"))
			for i := range reqs {
				at := fmt.Sprintf("2026-09-05T10:4%d:00Z", i)
				r := responseLine("scope-test", "one", fmt.Sprint(i), at, reqs[i], totals[i])
				appendAccounting(t, path, r)
				if incremental {
					scanResponses(t, st, home)
				}
				last := reqs[i]
				if i == 1 {
					// The mirror is not another model request; its large history is
					// deliberately excluded from the selective metadata reader.
					appendAccounting(t, path, `{"type":"compacted","payload":{"message":"`+strings.Repeat("x", defaultMaxRelevantRecord+1)+`","latest_token_usage_record":`+strings.TrimSuffix(strings.SplitN(r, `"payload":`, 2)[1], "}\n")+"}}\n")
					last = usage(0, 0, 0, 0, 0, 23163)
				}
				appendAccounting(t, path, tokenLine(at, old[i], last)+"\n")
				if incremental {
					scanResponses(t, st, home)
				}
			}
			scanResponses(t, st, home)
			accountingTotal(t, st, 534532)
			u, err := st.TurnUsage(context.Background(), "scope-test", "one")
			if err != nil || u != (model.TokenUsage{Input: 528307, CachedInput: 298752, Output: 6225, ReasoningOutput: 137, Total: 534532}) {
				t.Fatalf("%+v %v", u, err)
			}
			if ws, _ := st.Warnings(context.Background(), 100); len(ws) != 0 {
				t.Fatal(ws)
			}
			// Duplicate response delivery and a restored physical file must not add cost.
			appendAccounting(t, path, responseLine("scope-test", "one", "1", "2026-09-05T10:41:00Z", reqs[1], totals[1]))
			b, _ := os.ReadFile(path)
			if err := os.WriteFile(filepath.Join(home, "sessions", "restored.jsonl"), b, 0600); err != nil {
				t.Fatal(err)
			}
			if r := scanResponses(t, st, home); r.EventsInserted != 0 {
				t.Fatal(r)
			}
			accountingTotal(t, st, 534532)
			if r, err := (&Scanner{Store: st}).Scan(context.Background(), []string{home}, true); err != nil || r.Warnings != 0 {
				t.Fatalf("%+v %v", r, err)
			}
			accountingTotal(t, st, 534532)
		})
	}
}

func TestResponseLegacyTransitionAndTurnScope(t *testing.T) {
	st, home, path := accountingFixture(t)
	appendAccounting(t, path, accountingMeta+accountingTurn("old")+accountingToken(100, 100)+accountingTurn("mixed")+accountingToken(120, 20))
	scanResponses(t, st, home)
	// The first detailed record confirms an already-counted request. The
	// compaction has only an independent record and no legacy counterpart.
	appendAccounting(t, path, responseN("scope-test", "mixed", "normal", 20, 20)+responseN("scope-test", "mixed", "compact", 30, 50)+accountingToken(120, 20)+accountingTurn("old-again")+accountingToken(140, 20))
	scanResponses(t, st, home)
	accountingTotal(t, st, 170)
	appendAccounting(t, path, accountingTurn("new")+responseN("scope-test", "new", "new-normal", 10, 10)+accountingToken(10, 10))
	scanResponses(t, st, home)
	accountingTotal(t, st, 180)
	if ws, _ := st.Warnings(context.Background(), 100); len(ws) != 0 {
		t.Fatal(ws)
	}
}

func TestResponseForkOwnershipAndPartialAppend(t *testing.T) {
	st, home, path := accountingFixture(t)
	appendAccounting(t, path, `{"type":"session_meta","payload":{"id":"child","forked_from_id":"parent"}}`+"\n"+accountingTurn("parent-turn")+responseN("parent", "parent-turn", "parent-response", 100, 100))
	scanResponses(t, st, home)
	accountingTotal(t, st, 0)
	// Explicit response ownership can resolve a fork with no task_started or token_count.
	r := responseN("child", "child-turn", "child-response", 20, 20)
	appendAccounting(t, path, accountingTurn("child-turn")+r[:len(r)/2])
	scanResponses(t, st, home)
	accountingTotal(t, st, 0)
	appendAccounting(t, path, r[len(r)/2:])
	scanResponses(t, st, home)
	accountingTotal(t, st, 20)
	appendAccounting(t, path, responseN("parent", "parent-turn", "parent-again", 30, 130))
	scanResponses(t, st, home)
	accountingTotal(t, st, 20)
}

func TestResponseMissingPrefixAndUnmatchedLegacyAreVisible(t *testing.T) {
	st, home, path := accountingFixture(t)
	appendAccounting(t, path, accountingMeta+accountingTurn("one")+responseN("scope-test", "one", "second", 20, 100)+accountingToken(100, 20)+accountingToken(130, 30))
	scanResponses(t, st, home)
	accountingTotal(t, st, 100)
	ws, _ := st.Warnings(context.Background(), 100)
	kinds := map[string]bool{}
	for _, w := range ws {
		kinds[w.Kind] = true
	}
	if !kinds["response_usage_gap"] || !kinds["response_usage_unmatched"] {
		t.Fatal(ws)
	}
	// A late authoritative cumulative recovers the missing request without
	// summing its entire total again.
	appendAccounting(t, path, responseN("scope-test", "one", "third", 30, 130))
	scanResponses(t, st, home)
	accountingTotal(t, st, 130)
}

func TestResponseConflictRollsBackAndBadShapeDoesNotOwnTurn(t *testing.T) {
	st, home, path := accountingFixture(t)
	bad := strings.Replace(responseN("scope-test", "one", "bad", 10, 10), `"thread_id":"scope-test"`, `"thread_id":""`, 1)
	appendAccounting(t, path, accountingMeta+accountingTurn("one")+bad+accountingToken(10, 10))
	scanResponses(t, st, home)
	accountingTotal(t, st, 10)
	appendAccounting(t, path, responseN("scope-test", "one", "id", 10, 10))
	scanResponses(t, st, home)
	appendAccounting(t, path, responseN("scope-test", "one", "new", 20, 30)+responseN("scope-test", "one", "id", 11, 11))
	if _, err := (&Scanner{Store: st}).Scan(context.Background(), []string{home}, false); err == nil {
		t.Fatal("conflicting response identity accepted")
	}
	accountingTotal(t, st, 10)
}

func TestResponseAttributionDateAndMode(t *testing.T) {
	st, home, path := accountingFixture(t)
	appendAccounting(t, path, accountingMeta+accountingTurn("one")+responseLine("scope-test", "one", "a", "2026-09-08T01:00:00Z", usage(10, 5, 0, 2, 1, 12), usage(10, 5, 0, 2, 1, 12))+accountingTurn("two")+responseLine("scope-test", "two", "b", "2026-09-09T01:00:00Z", usage(20, 0, 0, 3, 1, 23), usage(20, 0, 0, 3, 1, 23)))
	scanResponses(t, st, home)
	summary, err := st.Summary(context.Background(), model.Filter{SinceDate: "2026-09-09", UntilDate: "2026-09-10"})
	if err != nil || summary.Usage.Total != 23 || summary.Modes.Fast.Total != 23 {
		b, _ := json.Marshal(summary)
		t.Fatalf("%s %v", b, err)
	}
}
