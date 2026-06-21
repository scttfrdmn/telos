# `bootstrap.acs` — design note

`bootstrap.acs` is the recursion's base case: the static seed the host
instantiates *before any planner runs* (architecture invariant 3 — the planner is
itself the root agent, bootstrapped from this seed). It is generated, never
hand-edited: [`cmd/genbootstrap`](../cmd/genbootstrap/main.go) constructs it in
Go, runs `acs.Validate`, and emits self-hashed JSON.

```sh
go run ./cmd/genbootstrap > bootstrap.acs   # or: make seed
```

## M3 redesign: the seed is a *minimal planning root*, not a hand-wired graph

Through M2 the seed was a hand-wired `scope→inquiry→reconcile→accept` graph. In
M3 the recursion actually closes, so the seed became the **smallest possible base
case**: a single **planning-root node** that, given a question, *emits* the real
multi-node graph (the produce→judge→settle inquiry), which the host
re-instantiates.

```
root  (KindPlan / PatternPlanning / replan:true)
  → reads the question
  → planner.Scope + Plan  →  a real acs.Spec  (the inquiry graph)
  → host re-instantiates and runs it
```

The decomposition is **not** in the seed — it is the planner's output
(`domain/research`). That is deliberate: **the seed fixes as little as possible so
it cannot bias the inquiry toward one archetype.** A composite question
("does X modulate Y, and what's the evidence") must be detected as composite at
*runtime*; a seed that hand-wired one shape would flatten it (§14 #1 failure,
upstream of any code).

## What the seed fixes — and only this

1. **The base case is a planning node.** `KindPlan` / `PatternPlanning` /
   `replan:true`: read the question, plan, re-instantiate. Nothing about *which*
   archetype — that is inferred per question.

2. **A fallback standard of proof** (`concordant`), used **only** when burn-rate
   has no reservoir-over-clock signal. As of M2, burn-rate is the *source* of the
   default standard (a visible function of grant phase); the seed value is the
   *fallback*. §15 fork #4 (how the per-question standard is *determined*) stays
   open.

3. **A grant slice** (`amount` + `period` — invariant 4, never a bare total) the
   emitted graph conserves against. Nominal placeholder; a real run's quote
   replaces it.

## What the seed does NOT fix

- **The inquiry shape.** Emitted per question by the planner (composite /
  mechanistic / evidence-synthesis), so composite detection and scoping land at
  runtime, not in the seed.
- **Acceptance placement.** Invariant 10 (acceptance in its own envelope) is the
  *planner's* responsibility to emit correctly, and `acs.Validate` rejects any
  emitted graph that violates it. The seam is enforced on *emission*, not just on
  hand-authoring — a stronger guarantee.

## How §14 rides on it

On the TREM2 string the planning root emits a **two-phase composite** graph
(evidence-synthesis substrate → mechanistic head), the scoping node bounds the
entity expansion between flatten and explode (inspectable), the mechanistic head
assembles for/against and the reconciliation returns an **earned, provenanced
Contested**, and the separate-envelope acceptance node **accepts** it — banking
surplus, with the lexicographic guard holding. The deterministic plumbing is
proven offline (`host/recursion_test.go`); the real-model quality is the
creds-gated gate (`TestSmoke_TREM2_Section14`).
