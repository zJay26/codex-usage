package model

import "encoding/json"

// SpawnParent accepts only explicit metadata, never titles, prompts or inferred
// naming conventions. Fork lineage is a separate relationship.
func SpawnParent(source json.RawMessage) string {
	var value struct {
		Subagent struct {
			ThreadSpawn struct {
				Parent string `json:"parent_thread_id"`
			} `json:"thread_spawn"`
		} `json:"subagent"`
	}
	if json.Unmarshal(source, &value) != nil {
		return ""
	}
	return value.Subagent.ThreadSpawn.Parent
}
