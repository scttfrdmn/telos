// Package binder turns an unbound ACS into a bound ACS: it resolves a model, a
// budget grant, and policy-gated tools for each node.
//
// The planner emits an unbound spec; the binder binds; the placer annotates.
// Capability is a constraint, not a model name — the binder asks the router to
// resolve acs.ModelConstraint, never asks a model whether it can do something.
// Tool grants are policy-gated (Cedar/LKI) here.
//
// Status: stub. In M1–M3 the host resolves models directly via the router and
// reserves grants via the governor at instantiation time (see host/build.go and
// host/planning_agent.go); a dedicated binding PASS that annotates the ACS before
// instantiation — and full Cedar/LKI tool gating — is deferred. The package is
// kept as the §2 seam so that pass has a home.
package binder
