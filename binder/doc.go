// Package binder turns an unbound ACS into a bound ACS: it resolves a model,
// a budget grant, and policy-gated tools for each node.
//
// The planner emits an unbound spec; the binder binds; the placer annotates.
// Capability is a constraint, not a model name — the binder asks the router to
// resolve acs.ModelConstraint, never asks a model whether it can do something.
// Tool grants are policy-gated (Cedar/LKI) here.
//
// Status: stub. Lands alongside the gateway/router work (M1) and the planner
// recursion (M3).
package binder
