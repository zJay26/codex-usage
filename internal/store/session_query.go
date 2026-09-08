package store

import "strings"

// Page cheap session IDs first, aggregate only their events, then join metadata
// once per session. Never join titles and paths to every event before grouping.
func sessionRowsSQL(where string) string {
	columns := []string{"input_tokens", "cached_input_tokens", "cache_write_input_tokens", "output_tokens", "reasoning_output_tokens", "total_tokens"}
	aggregates := []string{"e.session_id", "COUNT(*) event_count", "MAX(e.usage_at) last_usage",
		`CASE WHEN MAX(e.confidence='aggregate_only')=1 THEN 'aggregate_only' WHEN MAX(e.confidence='gap_fallback')=1 THEN 'gap_fallback' ELSE MAX(e.confidence) END confidence`}
	for _, c := range []string{"codex_home", "thread_title", "project_path", "model", "source", "agent_type"} {
		aggregates = append(aggregates, "MAX(e."+c+") "+c)
	}
	var values []string
	for _, c := range columns {
		aggregates = append(aggregates, "SUM(e."+c+") "+c)
		values = append(values, "a."+c)
	}
	values = append(values, "a.event_count", "a.confidence", "a.last_usage")
	for _, mode := range []string{"fast", "unknown"} {
		for _, c := range columns {
			name := mode + "_" + c
			aggregates = append(aggregates, "SUM(CASE WHEN e.service_mode='"+mode+"' THEN e."+c+" ELSE 0 END) "+name)
			values = append(values, "a."+name)
		}
	}
	return `WITH page AS MATERIALIZED (
		SELECT e.session_id,MAX(e.usage_at) last_usage FROM usage_events e WHERE ` + where + `
		GROUP BY e.session_id ORDER BY last_usage DESC,e.session_id LIMIT ? OFFSET ?
	), totals AS (
		SELECT ` + strings.Join(aggregates, ",") + ` FROM usage_events e
		JOIN page p ON p.session_id=e.session_id WHERE ` + where + ` GROUP BY e.session_id
	) SELECT COALESCE(NULLIF(a.session_id,''),'jsonl-unknown'),
		COALESCE(s.rollout_path,''),COALESCE(NULLIF(s.codex_home,''),a.codex_home,''),
		COALESCE(NULLIF(s.title,''),a.thread_title,''),COALESCE(NULLIF(s.project_path,''),a.project_path,''),
		COALESCE(NULLIF(s.model,''),a.model,''),COALESCE(NULLIF(s.source,''),a.source,''),
		COALESCE(s.thread_source,''),COALESCE(NULLIF(s.agent_type,''),a.agent_type,'main'),
		COALESCE(s.cli_version,''),COALESCE(s.tokens_used,0),COALESCE(s.created_at,0),COALESCE(s.updated_at,0),COALESCE(s.archived,0),
		` + strings.Join(values, ",") + ` FROM totals a LEFT JOIN sessions s ON s.session_id=a.session_id ORDER BY a.last_usage DESC,a.session_id`
}

func sessionSearchSQL(alias, search string) (string, []any) {
	pattern := "%" + strings.NewReplacer("~", "~~", "%", "~%", "_", "~_").Replace(search) + "%"
	// Search identifies sessions; the active date/model/mode filters select all
	// their usage. The same predicate is used by totals, pricing and exports.
	return alias + `.session_id IN (
		SELECT session_id FROM sessions WHERE session_id LIKE ? ESCAPE '~' OR title LIKE ? ESCAPE '~' OR project_path LIKE ? ESCAPE '~' OR model LIKE ? ESCAPE '~' OR source LIKE ? ESCAPE '~'
		UNION SELECT session_id FROM usage_events WHERE provenance='session_jsonl' AND (session_id LIKE ? ESCAPE '~' OR thread_title LIKE ? ESCAPE '~' OR project_path LIKE ? ESCAPE '~' OR model LIKE ? ESCAPE '~' OR source LIKE ? ESCAPE '~')
	)`, []any{pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern}
}
