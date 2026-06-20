// Package router selects a concrete model for a capability constraint under a
// budget ceiling — cascade-first, driven by surplus pressure.
//
// Capability-as-constraint: the router resolves acs.ModelConstraint to an
// acs.ModelBinding; callers never enumerate model names. Cascade-first means it
// prefers the cheapest model that can clear the bar and escalates only under
// pressure — the mechanism by which "complete with surplus" (invariant 8)
// reaches model selection.
//
// The interface (architecture §5):
//
//	Select(ctx, acs.ModelConstraint, ceil Budget) (acs.ModelBinding, error)
//
// Status: stub. A minimal selector lands with the gateway model path (M1); the
// full router table + cascade is additive (M7+).
package router
