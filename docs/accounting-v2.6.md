# v2.6 accounting and task relationships

## Counter scope and safe ingestion

The persisted accounting state distinguishes an unknown/session counter from a turn counter. A changed `turn_id` alone does not reset legacy session totals. A new turn whose complete `total_token_usage` equals `last_token_usage` establishes turn scope, including totals smaller than, equal to, or larger than the previous turn. Subsequent snapshots of that turn contribute only their delta. Missing subset fields retain the existing classification only within the same counter scope.

Turn-aware event identities also deduplicate copied rollouts. For proven turn counters created after migration, each file transaction checks the turn's existing ledger before reading its tail; a restored file that starts midway through a turn adds only the previously unseen increment. Session-cumulative writers keep the existing high-water behavior. Physical ownership remains the first `session_meta`; fork replay is excluded before accounting. Classification corrections for turn-scoped counters stay inside that turn, including fragments restored to another physical file.

Each file is processed in one SQLite `BEGIN IMMEDIATE` transaction. The writer lock is acquired before loading the cursor; usage events, classification corrections, turn modes, session progress and file offsets commit together. Database errors abort the file and surface as scan failures. The next scan retries the original offset. Syntax errors in complete records are warnings; incomplete EOF records keep their starting offset, even if `type` has not arrived. Oversized incomplete records also wait for a line boundary. Large unrelated records remain streamed and discarded.

This transaction is per physical file, so a large initial import can delay another writer. Readers use WAL snapshots and remain independent. A busy writer returns an error and retries on the next scan; it never advances a cursor past uncommitted usage.

## Upgrade boundary

Schema v9 is additive. Opening a v2.5.0 database preserves token amounts, timestamps, pricing classification, original local date labels and existing cursors. It adds accounting state, parent relationships, a persisted time zone and UTC hour identities. The Dashboard shows that pre-upgrade history has been retained. Metadata can be backfilled from retained sources without rebuilding token history.

The new parser applies to newly read records. It does not silently recalculate previously undercounted turns. Explicit `codex-usage scan --rebuild` recalculates only retained JSONL files and may lose entries whose original files have been deleted. Back up the state and verify source coverage first. A pre-upgrade session restored into a new physical file requests a rebuild when its old event identities cannot safely prove replay ownership.

## Query consistency and performance

- SQLite triggers advance `meta.data_revision` in the same commit as changes to events or session metadata. Every process observes this revision.
- Session rows and estimates are read from one WAL snapshot and return `data_revision`. The UI rejects an estimate from a different revision. Pricing overrides use a content hash, also exposed in status, so external config changes invalidate both server and browser caches.
- Search selects matching sessions by metadata or event model/source. Date, model, mode and other filters then select their usage consistently for totals, costs and exports. A session switching models does not display only the searched model's tokens while pricing all models.
- Session queries select a page of IDs, aggregate those events, then join metadata once per session. Pricing filters the raw indexed session ID rather than wrapping it in `COALESCE`.

Local comparison on 2026-09-08 used the same isolated SQLite snapshot: 114,960 events, 766 sessions. Each request started a fresh process, all requests missed application caches, and three runs per version were interleaved. Source discovery used an empty temporary Codex Home; production state was read through SQLite backup and was never migrated or rebuilt.

| Request, first 100 sessions, all time | v2.5.0 median | v2.6.0 median |
|---|---:|---:|
| `/sessions?include_estimate=0&limit=100&compact=1` | 2.7480 s | 0.4271 s |
| `/session-estimates?limit=100&compact=1&cost_basis=codex_fast_weighted` | 3.1189 s | 0.8265 s |

These are local latency observations, not a general performance guarantee. OS disk caches were not cleared. A SHA256 over the 27 existing `usage_events` fields, ordered by ID, stayed `2e1c2eb1d8b91c456814cc54cd7cc453b15f13104d010a64ae7a01331c52edf5` after migration; the 114,960 original events remained.

The final build reached health after its first schema migration in 5.87 seconds on that snapshot; subsequent fresh processes took 0.51–1.13 seconds. Hour identities use one bulk update when the historical UTC offsets have a common fractional-hour component, with a prepared-statement fallback for changing fractional offsets. Initialization allows up to one minute, including schema migration.

## Time zone contract

The first schema-v9 open saves one IANA accounting time zone in SQLite. It is detected from the host (including Windows-to-IANA mappings) or explicitly selected by `CODEX_USAGE_TIMEZONE` for a new database. Subsequent processes use the saved value even when their environment changes. Embedded tzdata keeps IANA lookup available on Windows. Existing stored day labels remain unchanged.

New events use that location for day labels and an actual UTC instant for each hour start. `/api/v1/timeseries?date=YYYY-MM-DD&bucket=hour&complete_hours=1` returns a `window` containing UTC `start`, exclusive `end`, and `complete_hours`. The browser uses this window and `point.time` as identities; `point.date` remains a display label and can repeat during a DST fall-back. A day can contain 23 or 25 hours. All date selectors and hour tooltips follow the accounting time zone shown in the footer, independent of the remote browser's zone.

## Task tree contract

`/api/v1/session-tree` accepts the same filters as sessions and paginates roots (maximum 100, UI 20), keeping each selected root's descendants together. It returns a flat preorder with `parent_id`, `depth`, `children`, `context_only`, `subtree_usage`, and `subtree_modes`.

Parents come only from `source.subagent.thread_spawn.parent_thread_id` or schema-probed `thread_spawn_edges` metadata in a read-only Codex state DB. Conflicting multiple parents in the edge table are not guessed. Fork lineage is stored separately as `forked_from_id`. Retained JSONL metadata can fill relationships without modifying token history.

`usage` and `estimate` are the node's own filtered usage and cost. `subtree_usage` includes its descendants under the current filter. Ancestors outside the filter are context-only nodes with zero own usage. Missing parents and cycles are shown as detached roots with a diagnostic label. Summing every node's own usage equals the sum of root subtotals; summing all subtotals would double-count descendants.

## Verification

Regression coverage includes smaller/equal/larger new-turn counters, scanner restart, copied rollouts, concurrent writers, storage rollback and retry, partial prefixes, split reader fragments, multi-model search/pricing, cross-connection cache invalidation, external pricing changes, read snapshots, DST and fractional offsets, task-tree totals/cycles/orphans, and remote-browser hourly charts. Playwright checks both the real server and synthetic tree UI, including keyboard collapse and mobile dark mode.

CI runs Go tests/vet on Windows, Linux, macOS arm64 and macOS Intel. Both macOS runners also install a temporary LaunchAgent, check HTTP health, uninstall while preserving its database, reinstall, and check health again. Release builds use Go 1.26.8 and publish six explicitly named binaries plus checksums.
