// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"sort"

	"github.com/scttfrdmn/telos/acs"
)

// GraphSummary is a compact description of the instantiated graph, returned in
// an invocation response so a caller can verify WHICH composition ran. This is
// the observable evidence that M0's job — instantiate the seed graph — happened.
type GraphSummary struct {
	Prompt    string        `json:"prompt"`
	Archetype string        `json:"archetype"`
	Standard  string        `json:"standard"`
	RootID    string        `json:"root_id"`
	Hash      string        `json:"hash"`
	NodeCount int           `json:"node_count"`
	Nodes     []NodeSummary `json:"nodes"`
	Budget    BudgetSummary `json:"budget"`
}

// NodeSummary describes one instantiated node.
type NodeSummary struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Pattern  string   `json:"pattern"`
	Trust    string   `json:"trust"`
	Children []string `json:"children,omitempty"`
}

// BudgetSummary surfaces the grant as amount + period (never a bare total —
// invariant 4), plus the derived daily rate.
type BudgetSummary struct {
	Amount      float64 `json:"amount"`
	PeriodHours float64 `json:"period_hours"`
	RatePerDay  float64 `json:"rate_per_day"`
	Currency    string  `json:"currency"`
}

func summarize(s *acs.Spec) GraphSummary {
	ids := make([]string, 0, len(s.Nodes))
	for id := range s.Nodes {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)

	nodes := make([]NodeSummary, 0, len(ids))
	for _, id := range ids {
		n := s.Nodes[acs.NodeID(id)]
		children := make([]string, 0, len(n.Children))
		for _, c := range n.Children {
			children = append(children, string(c))
		}
		nodes = append(nodes, NodeSummary{
			ID:       string(n.ID),
			Kind:     string(n.Kind),
			Pattern:  string(n.Pattern),
			Trust:    string(n.Trust),
			Children: children,
		})
	}

	return GraphSummary{
		Prompt:    s.Prompt,
		Archetype: string(s.Archetype),
		Standard:  string(s.DefaultStandard()),
		RootID:    string(s.RootID),
		Hash:      s.Hash,
		NodeCount: len(s.Nodes),
		Nodes:     nodes,
		Budget: BudgetSummary{
			Amount:      s.Budget.Amount,
			PeriodHours: s.Budget.Period.Hours(),
			RatePerDay:  s.Budget.RatePerDay(),
			Currency:    s.Budget.Denomination(),
		},
	}
}
