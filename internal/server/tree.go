package server

import (
	"context"
	"net/http"
	"sort"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/pricing"
	"github.com/zJay26/codex-usage/internal/store"
)

type taskNode struct {
	store.SessionRow
	ParentID           string           `json:"parent_id,omitempty"`
	Depth              int              `json:"depth"`
	Children           int              `json:"children"`
	ContextOnly        bool             `json:"context_only"`
	RelationshipStatus string           `json:"relationship_status,omitempty"`
	SubtreeUsage       model.TokenUsage `json:"subtree_usage"`
	SubtreeModes       model.ModeUsage  `json:"subtree_modes"`
	Estimate           pricing.Estimate `json:"estimate"`
}

// buildTaskTree retains ancestors as context-only nodes under filters. Invalid
// edges become visible roots; no guessed ownership and no recursive traversal.
func buildTaskTree(rows []store.SessionRow, metadata map[string]model.SessionInfo) (map[string]*taskNode, map[string][]string, []string) {
	nodes := map[string]*taskNode{}
	for _, row := range rows {
		meta := metadata[row.SessionID]
		row.ParentSessionID, row.ForkedFromID = meta.ParentSessionID, meta.ForkedFromID
		nodes[row.SessionID] = &taskNode{SessionRow: row, ParentID: meta.ParentSessionID, SubtreeUsage: row.Usage, SubtreeModes: row.Modes}
	}
	for _, row := range rows {
		id := nodes[row.SessionID].ParentID
		for id != "" {
			if _, ok := nodes[id]; ok {
				break
			}
			meta, ok := metadata[id]
			if !ok {
				break
			}
			nodes[id] = &taskNode{SessionRow: store.SessionRow{SessionInfo: meta}, ParentID: meta.ParentSessionID, ContextOnly: true}
			id = meta.ParentSessionID
		}
	}
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	visited := map[string]bool{}
	for _, start := range ids {
		path := []string{}
		active := map[string]bool{}
		for id := start; id != "" && !visited[id]; {
			n := nodes[id]
			if n == nil {
				break
			}
			if active[id] {
				n.ParentID = ""
				n.RelationshipStatus = "cycle"
				break
			}
			active[id] = true
			path = append(path, id)
			if n.ParentID != "" && nodes[n.ParentID] == nil {
				n.ParentID = ""
				n.RelationshipStatus = "missing_parent"
			}
			id = n.ParentID
		}
		for _, id := range path {
			visited[id] = true
		}
	}
	children := map[string][]string{}
	for _, id := range ids {
		n := nodes[id]
		children[n.ParentID] = append(children[n.ParentID], id)
	}
	order := []string{}
	stack := append([]string{}, children[""]...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		order = append(order, id)
		stack = append(stack, children[id]...)
	}
	for i := len(order) - 1; i >= 0; i-- {
		n := nodes[order[i]]
		n.Children = len(children[n.SessionID])
		if p := nodes[n.ParentID]; p != nil {
			p.SubtreeUsage = p.SubtreeUsage.Add(n.SubtreeUsage)
			p.SubtreeModes.Fast = p.SubtreeModes.Fast.Add(n.SubtreeModes.Fast)
			p.SubtreeModes.Unknown = p.SubtreeModes.Unknown.Add(n.SubtreeModes.Unknown)
			p.SubtreeModes.Complete(p.SubtreeUsage)
			if n.LastUsage.After(p.LastUsage) {
				p.LastUsage = n.LastUsage
			}
		}
	}
	for _, siblings := range children {
		sort.Slice(siblings, func(i, j int) bool {
			a, b := nodes[siblings[i]], nodes[siblings[j]]
			if a.LastUsage.Equal(b.LastUsage) {
				return a.SessionID < b.SessionID
			}
			return a.LastUsage.After(b.LastUsage)
		})
	}
	return nodes, children, children[""]
}

func (s *Server) handleSessionTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	filter, limit, offset, _, err := sessionRequestParameters(r, s.Store.Location())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if limit > 100 {
		limit = 100
	}
	items := []taskNode{}
	rootsTotal := 0
	var revision uint64
	err = s.Store.ReadSnapshot(r.Context(), func(data *store.Store) error {
		var err error
		revision, err = data.DataRevision(r.Context())
		if err != nil {
			return err
		}
		rows, err := data.Sessions(r.Context(), filter, -1, 0)
		if err != nil {
			return err
		}
		metadata, err := data.SessionRelationships(r.Context())
		if err != nil {
			return err
		}
		nodes, children, roots := buildTaskTree(rows, metadata)
		rootsTotal = len(roots)
		end := min(offset+limit, len(roots))
		if offset >= end {
			return nil
		}
		type entry struct {
			id    string
			depth int
		}
		stack := []entry{}
		for i := end - 1; i >= offset; i-- {
			stack = append(stack, entry{roots[i], 0})
		}
		for len(stack) > 0 {
			e := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			n := *nodes[e.id]
			n.Depth = e.depth
			n.Title = compactText(n.Title, 240)
			items = append(items, n)
			for i := len(children[e.id]) - 1; i >= 0; i-- {
				stack = append(stack, entry{children[e.id][i], e.depth + 1})
			}
		}
		return s.priceTree(r.Context(), data, filter, items)
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "root_count": rootsTotal, "offset": offset, "limit": limit, "data_revision": revision})
}

func (s *Server) priceTree(ctx context.Context, data *store.Store, filter model.Filter, items []taskNode) error {
	overrides, err := s.pricingOverrides()
	if err != nil {
		return err
	}
	for start := 0; start < len(items); start += 500 {
		end := min(start+500, len(items))
		builders := map[string]*pricing.Builder{}
		ids := []string{}
		for _, item := range items[start:end] {
			b, err := pricing.NewBuilderForBasis(overrides, filter.CostBasis, data.Location())
			if err != nil {
				return err
			}
			builders[item.SessionID] = b
			ids = append(ids, item.SessionID)
		}
		if err := data.WalkSessionPricingAggregates(ctx, filter, ids, func(event model.UsageEvent) error { return builders[event.SessionID].Add(event) }); err != nil {
			return err
		}
		for i := start; i < end; i++ {
			items[i].Estimate = builders[items[i].SessionID].Report().Summary
		}
	}
	return nil
}
