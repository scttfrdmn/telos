// Package planner is the root agent: question → ACS.
//
// The planner is not a privileged outside controller — it runs ON the host as a
// Planning-pattern node, budgeted from the run's envelope, bootstrapped from the
// static seed (bootstrap.acs). Planning, re-planning, and sub-planning are one
// operation at different depths (invariant 3).
//
// It is estimate-first (architecture §3): the opening act of a run is a cheap
// scoping pass that turns the question into a costed plan, not execution. The
// quote is itself plan-adjudicated so the planner cannot low-ball to get
// authorized.
//
// The interface (architecture §5):
//
//	Scope(ctx, prompt string, Policy) (*Quote, error) // estimate-first
//	Plan(ctx, *Quote) (*acs.Spec, error)              // root agent
//
// Status: stub. Lands in M3 (the keystone) with one archetype
// (evidence-synthesis), composite detection, and a minimal scoping/quote pass.
package planner
