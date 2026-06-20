# `bootstrap.acs` — design note

`bootstrap.acs` is the recursion's base case: the hand-authored seed the host
instantiates *before any planner exists* (architecture invariant 3 — the planner
is itself the root agent, bootstrapped from this seed). It is generated, never
hand-edited: [`cmd/genbootstrap`](../cmd/genbootstrap/main.go) constructs the
spec in Go, runs `acs.Validate`, and emits the self-hashed JSON.

```sh
go run ./cmd/genbootstrap > bootstrap.acs
```

It looks like boilerplate. It is not — it silently fixes two system-wide defaults
that every later run inherits unless something overrides them. That is why it
gets explicit design rather than a stub (hand-off note).

## Default 1 — decomposition (how Telos thinks when nothing tells it how)

The seed encodes the **evidence-synthesis** shape (architecture §10/§11) fronted
by an **estimate-first** scoping pass (§3):

```
root (Sequential, Plan, replan)        ← the planner-as-root spine
├── scope      (React, Reason)         ← estimate-first: question → costed plan
├── inquiry    (Supervisor, Reason)    ← the default decomposition
│   ├── retrieve   (leaf, Retrieve)        source fan-out
│   ├── extract    (Parallel, Reason)      independent per-source extraction
│   │   ├── extract_a (leaf)
│   │   └── extract_b (leaf)
│   └── synthesize  (leaf, Reason)         consolidate with provenance
├── reconcile  (leaf, Reconcile)       ← may return *contested*, not a forced yes/no
└── accept     (leaf, Acceptance)      ← SEPARATE ENVELOPE — judges, never produces
```

This is the shape the planner reaches for by default. Estimate-first ordering is
load-bearing: the first deliverable of any question is the cost of answering it,
so `scope` runs before any worker spends and is held to the trivial `scoping`
standard so it can never itself become a real spend.

## Default 2 — standard of proof (how sure we need to be)

The seed fixes `standard: "concordant"` as the **system default**:

| Standard | Meaning |
|---|---|
| `scoping` | trivial bar — enough to produce a costed plan |
| `plausible` | internally coherent, lightly sourced |
| **`concordant`** | **default** — grounded in direction-neutral verifiable facts (cited sources exist and say what's claimed; computations reproduce) |
| `oracle` | oracle-verified where an oracle exists (clinical-grade) |

`concordant` is chosen deliberately: not `oracle` (too costly as a standing
default — `burnrate` would decline it late in a grant), not bare assertion. It
preserves *contested* and *negative* as first-class results, which §14 requires.

**Open fork, not decided here:** *how* the per-question standard is determined
(prompt vs archetype vs adjudicated, and how question-stakes combine with
grant-burn) is architecture §15 #4. The seed only fixes the floor the recursion
bottoms out on; `burnrate` (M2) modulates it.

## The keystone seam — acceptance in a separate envelope (invariant 10)

`accept` is a node of kind `acceptance` with `trust: "isolated"`, wired as a
**sibling** of the production spine under the neutral `root` coordinator, and fed
the reconciled record by a one-way dataflow edge (`reconcile → accept`).

- The root is a *neutral coordinator*, not a producer of the record — wiring
  production and a separately-enveloped judge as siblings is the **correct**
  invariant-10 shape, enforced by `acs.Validate`.
- In **M0 the acceptance node is inert** (no verdict logic until M2). What must
  be right *now* is the separation — it is unrecoverable later. The complementary
  half (production code can never import acceptance to self-render) is enforced at
  the package boundary, not the ACS (see issue #9).

## Budget — a grant, never a total (invariant 4)

Every budget in the seed is `{amount, period}` — a grant rate, not a bare total.
The seed's grant ($100 over 24h) is a well-formed placeholder; a real run
replaces it via the planner's scoping/quote pass. `acs.Budget.Validate` rejects a
zero period precisely so a "total" can never enter the system.

## Patterns exercised

All four M0 patterns appear, each for a real reason (not to tick a box):

| Pattern | Node | Why |
|---|---|---|
| Sequential | `root` | estimate-first ordering: scope → inquire → reconcile → accept |
| React | `scope` | reason + cheap probes to produce a costed plan (tool-driven, bounded) |
| Supervisor | `inquiry` | plan / delegate / synthesize — the evidence-synthesis default |
| Parallel | `extract` | independent per-source extraction (the goroutine-rung fan-out) |
