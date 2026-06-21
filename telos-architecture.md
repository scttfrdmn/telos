# Telos — Architecture (v0.2.1)

> A research question synthesizes a budgeted agent graph that investigates it
> autonomously over heterogeneous runtimes — stopping intelligently, completing
> with surplus, returning honest results including negatives, and spending a real
> grant across its full period rather than to a wall.

*Telos: the system is named for its load-bearing invariant — the original
question is the fixed purpose everything serves and nothing is allowed to
capture. Adversarial in method, shared in telos.*

---

## 0. What it is

The user writes a **question**, not a structure. A planner infers the shape of
the inquiry and emits a composition spec (agenkit patterns stitched into a
graph). Each node is bound to a model and a conserved budget, placed on the
cheapest runtime that satisfies its trust / locality / resource constraints, and
run with every unit of metered work — model calls *and* synthesized computation —
passing through one chokepoint. The graph reconciles against one ledger.

Money is the clock — literally: the budget is a grant, an amount over a period,
and both axes are conserved. agenkit is the instruction set. cohort is the
reconciler. AgentCore and spore.host are interchangeable backends. The planner is
the only new "smart" part; everything under it is retargeted Playground Logic
infrastructure. Generic core; research focus lives in one swappable domain pack.

Shape-similar to ARPA-H IGoR (SOL-26-155): generate hypotheses, design and
conduct experiments, refine models — 10x faster, reproducible, gold-standard.
The compute-synthesis layer (§7) is what makes this IGoR-shaped rather than a
literature-search tool.

---

## 1. Design invariants

1. **The telos is fixed; the mechanism varies.** The original question is the
   purpose every node serves and nothing may capture. The prompt is a question,
   not a structure: the user owns the *what*, the planner owns the *how*.
   Question → inquiry shape → workflow → verification structure → routing
   economics is the planner's core inference.
2. **Composition, not codegen.** The prompt emits *data* (an ACS); a generic host
   instantiates patterns from it. Build-per-prompt is reserved for spill nodes.
3. **The planner is the root agent.** It runs *on* the host as a Planning-pattern
   node, budgeted from the run's envelope, bootstrapped from a static seed ACS
   (`bootstrap.acs`). No privileged outside. Planning, re-planning, and
   sub-planning are one operation at different depths.
4. **Budget is a grant: amount AND time, jointly conserved.** The conserved
   quantity is dollars-over-period, not dollars. Both underspend and overspend are
   failures. Within a run, Σ(child) ≤ parent, recursively; escrow before, settle
   after. Exhaustion is the backstop exit, not the plan.
5. **One work chokepoint.** No agent gets raw model or compute access. Every
   metered unit — model call or synthesized computation — routes → escrows →
   meters → settles at the gateway. The only place local models and off-platform
   compute *can* be metered.
6. **Transport is a placement decision, not a code change.** Same agenkit object
   → goroutine | A2A session | instance, chosen by the placer.
7. **The launch is easy; the Observer is the design.** Every substrate adapter is
   defined by its readiness/lifecycle signal.
8. **Complete with surplus.** The budget is a ceiling, not a target. Among
   *accepted* outcomes, prefer the one leaving the most margin: stop at the
   marginal-value-per-dollar knee, not the wall. Surplus is rewarded and is an
   upward signal — strictly gated by acceptance. Surplus on an unaccepted result
   is abandonment, not thrift, and banks nothing. Per-question surplus funds the
   *next* question within the same grant.
9. **Four exits, ranked.** done · handoff · honest-negative · exhaustion. The
   first three are completions that bank surplus; exhaustion is the lowest-reward
   exit. Negatives and "contested" are first-class results.
10. **Acceptance is rendered by a disinterested party, in a separate envelope.**
    A node never settles its own acceptance. The verdict is rendered by a party
    with no stake in which way the result goes, funded from a neutral node, and it
    is grounded in direction-neutral verifiable facts (a cited source exists and
    says what's claimed; a computation reproduces) — labeled oracle-verified /
    concordant-under-test / contested, never asserted as "true" where no oracle
    exists. **(Hard seam — see §12.)**
11. **Go by default.** Goroutines = near-free in-envelope fan-out + free CPU-idle;
    `context.Context` = budget envelope + kill-switch as primitives. Other
    agenkit languages are the spill hatch.

---

## 2. Component catalog

| Component | Role | Status | Module |
|---|---|---|---|
| **planner** | question → ACS (root agent; archetype inference; scoping/quote) | new | `telos/planner` |
| **domain/research** | question-archetype → ACS-shape pack (only research-aware part) | new | `telos/domain/research` |
| **acs** | composition spec schema + validation | new | `telos/acs` |
| **binder** | ACS → bound ACS (model + budget + tools per node) | new | `telos/binder` |
| **router** | model selection; cascade-first (surplus pressure) | new (reckoner/ephemeron) | `telos/router` |
| **gateway** | the work chokepoint: route/escrow/meter/settle for model *and* compute | new | `telos/gateway` |
| **governor** | admission + conservation + ledger + burst + surplus objective | ASBB, retargeted | `telos/governor` |
| **burnrate** | reservoir-over-clock controller; modulates default standard of proof | new | `telos/burnrate` |
| **placer** | bound node → transport + substrate | new (truffle rungs + governor) | `telos/placer` |
| **acceptance** | disinterested verdict node, separate trust + budget envelope | new (hard seam; policy open) | `telos/acceptance` |
| **cohort** | reconcile the named set across substrates | to be extracted (not yet published; placeholder in `substrate/inproc` until M4 — telos#12) | `spore-host/cohort` |
| **host** | generic agenkit-go runtime on the AgentCore contract | new | `telos/host` |
| **substrate/inproc** | goroutine supervisor adapter | new | `telos/substrate/inproc` |
| **substrate/agentcore** | AgentCore Actuator + Observer | new | `telos/substrate/agentcore` |
| **substrate/sporehost** | spore.host Actuator + Observer; compute backend via MCP | new | `telos/substrate/sporehost` |
| **attest** | provenance + reproducibility for emitted claims | provabl/attest/Copland | existing |
| **ledger** | distributed conservation + WAL + surplus + neutral pool | new (in governor) | `telos/ledger` |

Imports agenkit and `spore-host/cohort` as libraries. The agenkit Go module is
`github.com/scttfrdmn/agenkit-go` (a distribution mirror; module at repo root) —
the canonical monorepo path `github.com/scttfrdmn/agenkit/agenkit-go` is not
`go get`-able because committed binaries exceed Go's 500 MB zip ceiling
(scttfrdmn/agenkit#660). Swap back to the canonical path once cleaned (telos#13).
`spore-host/cohort` is **not yet published** — it must be extracted before M4
(telos#12); `substrate/inproc` runs on a local placeholder until then. Go 1.26,
Apache 2.0. State in GitHub Issues / Projects / Milestones.

---

## 3. Funding model — where the currency actually comes from

The internal economy (surplus, burst, bonds, forfeitures) is **recirculation**;
none of it creates currency. Currency enters from exactly one exogenous source,
and the stack terminates in three layers:

1. **Grant** — the exogenous reservoir-and-clock: an amount over a period. Real
   dollars, real deadline, justified to no one inside Telos. This is the floor;
   the turtles stop here.
2. **Standing policy** — the PI's one-time posture over the grant: target burn
   rate, default standard of proof for the program's work, escalation thresholds.
   Set once, not per question.
3. **Per-question draw** — automatic admission against *remaining reservoir over
   remaining clock*, estimate-scoped by the planner's scoping pass, escalated to
   the human only when *exceptional* (disproportionate slice, high-value override,
   or change-order overrun).

The user denominates in what they understand — a **ceiling**, a **tier**, or a
**confidence target** — not tokens. The standard of proof is therefore the user's
*primary* input (the one thing they genuinely know: how sure they need to be);
envelope, bond curve, and court tier all derive from it.

**Estimate-first.** The opening act of a run is not execution but a cheap scoping
pass (bounded by a trivial scoping allowance) that turns the question into a
*costed plan*: archetype, shape, estimated envelope, implied standard of proof,
and cheaper-shallow / costlier-rigorous alternatives. The quote is itself
plan-adjudicated (pre-registration, §12) so the planner can't low-ball to get
authorized. On overrun that's genuine (question harder than scoping could see,
not padding), the system **stops and re-authorizes** — a change order — rather
than silently overspending or failing.

> The first deliverable of any question is the cost of answering it. "What would
> it cost to resolve X to clinical confidence?" is independently valuable —
> sometimes more than the answer.

---

## 4. The ACS — central data structure

Data, content-hashable, versionable. Planner emits unbound; binder binds; placer
annotates. A `bootstrap.acs` ships with the host as the recursion's base case
(see hand-off note).

```go
type Spec struct {
    Version   string
    Hash      string
    Prompt    string          // the question (provenance / telos)
    Archetype Archetype       // inferred inquiry shape (research domain pack)
    RootID    NodeID
    Nodes     map[NodeID]*Node
    Edges     []Edge
    Budget    Budget          // amount + time slice drawn from the grant
}

type Node struct {
    ID       NodeID
    Kind     NodeKind         // Reason | Retrieve | ComputeSynthesis | Plan | Reconcile | Acceptance
    Pattern  Pattern
    Role     string
    Tools    []ToolRef        // policy-gated by binder (Cedar/LKI)
    Model    ModelConstraint  // capability constraint, not a model name
    Budget   BudgetRequest
    Trust    TrustBoundary    // same-envelope | isolated | untrusted
    Gravity  Gravity          // data/model/compute locality → placement
    Replan   bool             // may emit a sub-ACS (set by archetype, not generic)
    Children []NodeID

    Binding    *ModelBinding
    Grant      *BudgetGrant
    Placement  *Placement
    Provenance *Provenance     // sources + attestations threaded up to claims
}

type Edge struct {
    From, To NodeID
    Kind     EdgeKind          // Dependency | Dataflow
    Ref      StateRef          // dataflow passes state BY REFERENCE
}
```

State-by-reference (footprint stays the working slice); capability-as-constraint
(router resolves; never ask a model if it can); Trust/Gravity decide placement,
not cost; **Provenance is first-class** — the citation/attestation graph *is* the
deliverable, unprovenanced claims fail acceptance; **ComputeSynthesis** emits a
*workload spec*, not a message; **Acceptance** is a node kind in a separate
envelope (inv. 10).

---

## 5. Core interfaces

```go
type Planner interface {
    Scope(ctx context.Context, prompt string, policy Policy) (*Quote, error)   // estimate-first
    Plan(ctx context.Context, q *Quote) (*acs.Spec, error)                     // root agent
}

type Router interface {
    Select(ctx context.Context, c acs.ModelConstraint, ceil Budget) (acs.ModelBinding, error)
}

type Governor interface {
    Admit(ctx context.Context, q *Quote, reservoir Reservoir, clock Clock) (Admission, error) // rate-aware
    Reserve(ctx context.Context, parent GrantID, req BudgetRequest) (*BudgetGrant, error)      // Σ child ≤ parent; fails closed
    Settle(ctx context.Context, g GrantID, actual Cost, o Outcome) error                       // surplus banks iff Accepted
    Release(ctx context.Context, g GrantID) error
    Remaining(g GrantID) Budget
}

type BurnRate interface {
    // landing controller: modulates default standard of proof so the grant
    // lands near-zero at the deadline — neither starving early nor dying rich.
    DefaultStandard(reservoir Reservoir, clock Clock) StandardOfProof
}

// gateway: one chokepoint for ALL metered work.
type Gateway interface {
    Invoke(ctx, acs.ModelBinding, ModelRequest) (ModelResponse, Cost, error)   // model call
    RunWork(ctx, WorkloadSpec) (WorkResult, Cost, error)                       // synthesized compute (spore.host MCP)
}

type Placer interface {
    Place(ctx context.Context, n *acs.Node) (acs.Placement, error)
}

type Substrate interface {
    cohort.Actuator
    cohort.Observer    // ← the design work
    Transport() acs.Transport
}

// Acceptance: rendered by a disinterested node in a separate envelope (inv. 10).
type Acceptance interface {
    Render(ctx context.Context, record Record, standard StandardOfProof) (Verdict, error)
}

type Verdict struct {
    Accepted bool
    Basis    Basis    // OracleVerified | ConcordantUnderTest | Contested
    Note     string
}

// Outcome: the four exits, returned by every node.
type Outcome struct {
    Exit     ExitKind   // Done | Handoff | Negative | Exhausted
    Accepted bool       // from a Verdict, never self-rendered
    Surplus  Budget     // grant − actual (credited only if Accepted)
    Cause    string     // why surplus → planner/burnrate feedback
    Handoff  *HandoffTo
}
```

Budget rides in `context.Context` in-process; across A2A it is explicit wire
fields. `ctx` cancel is the in-process kill-switch; exhaustion cancels it.

---

## 6. Execution model & termination

Per metered unit, at the gateway:

```
estimate worst-case (tokens: output ← max_tokens; compute: reckoner estimate)
  → governor.Reserve(escrow)         // fails closed on conservation breach
  → route (model) / pick frontier point (compute)
  → invoke / run (cache-aware)
  → meter actual                     // local + off-platform metered HERE
  → governor.Settle(actual, outcome) // surplus banks iff accepted (by §12 node)
```

Termination is an *output of the work*, never a target. Continue only while
marginal value per dollar exceeds the knee; then choose an exit. **stall
detection** (no progress over N iterations) forces handoff-or-negative;
**surplus-favoring** (inv. 8) makes the agent seek the knee and prefer cheap soft
exits; **negative-results bank** makes honest "no / contested / infeasible" a
high-reward completion.

---

## 7. Transport ladder

| Rung | When | Cost shape |
|---|---|---|
| **goroutine** | same trust + budget tree; I/O-bound fan-out | one baseline + one 8 GB pool; near-zero marginal; free CPU-idle |
| **AgentCore session** (A2A) | isolation / >2 vCPU / >8 GB / untrusted / trust boundary | own microVM; active-CPU + peak-mem meter; 8 GB / 8 h caps |
| **spore.host instance** (A2A/hyphae) | GPU-in-process / huge memory / local model / sovereign data / heavy synthesized compute | owned hardware; memory rectangle sunk not metered; spored lifecycle |

First trigger that fires wins; goroutine is the default. Justification bar rises
per rung.

---

## 8. Compute synthesis & the spore.host compute backend

The planner synthesizes *agent structure*; agents synthesize *optimized
computation* at runtime — deferred because the right computation isn't knowable
until an agent faces its sub-question.

- A **ComputeSynthesis** agent emits a **WorkloadSpec** (method, data-by-ref,
  resource requirements, precision/kernel choices) — the artifact fieldcraft /
  queuezero already consume. Agent = workload *generator* over the existing
  *runner*.
- **Two nested optimizations, separate.** *Science* (method/model/data) is the
  agent's; *execution* (instance, precision, pipeline, parallelism) is
  reckoner/truffle/advise's dominance-frontier search — the agent *calls* it.
- **Exploration is metered**: config-space search depth is gated by the value at
  stake (cheap → closed-form pick; expensive Trainium job → real frontier search).
- **Budget unifies**: compute is estimated (reckoner), escrowed (governor),
  metered (gateway) like a model call. Three spend types — model calls, spawns,
  synthesized compute — one chokepoint. Deciding to spend compute is a high-value
  admission gate.
- **Backend = spore.host on AWS, via MCP**, registered as an AgentCore Gateway
  target, Cedar/LKI gating compute-spend authority. *Expansion needed:* expose the
  suite as tools — `truffle` (rungs), `reckoner` (frontier), `spawn`/`queuezero`/
  `cohort` (provision+run), `spored` (observe), teardown (lifecycle).
- **Attestation**: generated results must be attested + reproducible
  (provabl/attest/Copland; stegano for synthetic intermediates).

Self-similar: planner emits ACS → host instantiates agents; ComputeSynthesis
emits WorkloadSpec → spore.host instantiates compute. Telos is the question-driven
front end to the whole back-end portfolio; ComputeSynthesis is the seam.

---

## 9. Governor, ledger & burn-rate (ASBB, retargeted)

Your Slurm budget machine pointed at agents and compute, now grant-aware.

- **Conservation:** Σ(child) ≤ parent remaining; fails closed; recurses; max depth.
- **Rate, not total:** ASBB was always a spend-*rate* admission controller. The
  grant's amount-over-period is the rate it governs. `burnrate` watches
  reservoir-over-clock and sets the **default standard of proof** so the grant
  lands near-zero at the deadline — a clinical-grade answer affordable in month 2
  is declined in month 11.
- **Objective is lexicographic:** (1) attested acceptance (§12), (2) maximize
  surplus. Never a weighted sum (under-deliver exploit).
- **Savings classes:** *slack* banks only on acceptance; *caching* banks
  unconditionally.
- **Surplus is a signal** (`Outcome.Cause`) → planner/burnrate recalibrate;
  surplus → burst makes easy branches subsidize hard ones (market load-balancing).
  Within a grant, surplus funds the next question.
- **Neutral pool:** forfeitures/penalties (when the court layer lands) route to a
  neutral court fund, never to a judge or winning advocate.
- **Distributed:** in-proc invariant/cancel free; across A2A on the wire
  (`{grant_id, reservation, deadline, cancel_token}` out;
  `{result, cost_settled, outcome, child_settlements[]}` back), reconciled via WAL.

---

## 10. Research domain pack — archetypes

The only research-aware component. Maps archetypes → ACS shapes, each carrying a
verification structure (sets cascade aggressiveness → budget shape):

- **Evidence synthesis** → retrieve fan-out → parallel extraction → supervisor
  consolidation. Large verification gap → cascades well.
- **Mechanistic/causal** → hypothesis decomposition → adversarial for/against →
  reconciliation that may return *contested*. (IGoR TA1.) Replan-capable.
- **Comparative** → parallel per-arm → normalize → difference.
- **Quantitative/reproduction** → ComputeSynthesis nodes → spore.host rungs.
  (IGoR TA1/TA2.) Replan-capable.
- **Exploratory/open** → weak verification; emit a *scoping node first* that
  narrows before any worker spends. Replan-capable.

Composite questions are normal: *"does TREM2 modulate tau propagation, and what's
the evidence"* = evidence-synthesis substrate → mechanistic-reconciliation head.

---

## 11. v0 cut — buildable spine

- generic `host` on the AgentCore contract, 4 patterns (Sequential, Parallel,
  Supervisor, React) from an ACS; ships `bootstrap.acs`.
- research planner with **one archetype** (evidence-synthesis) + composite
  detection + a minimal **scoping/quote** pass.
- `gateway`: route+meter+escrow over Bedrock + one local endpoint (model path);
  compute path (`RunWork`) stubbed.
- `governor`: flat per-run admission, **rate-aware against a grant
  (amount+period)**, conservation on spawns, lexicographic acceptance→surplus,
  in-mem + WAL ledger.
- `acceptance`: a **separate-envelope** verdict node (summary-judgment only;
  courtroom policy deferred) — built separate from production from day one.
- `placer`: two transports (goroutine, A2A-to-new-session).
- four-exit Outcome with negative-results banking.

Deferred (additive): full court (advocates/tiers/bonds) · compute-synthesis +
spore.host MCP backend · attestation layer · burst pool · local-model + Trainium
rungs · full router table + cascade · Cedar/LKI gating · lagotto · overcommit +
degradation · more archetypes.

---

## 12. Adjudication — direction of travel (policy open, seam pinned)

**Committed seams (build these now):**
- Acceptance is a **separate node kind in a separate trust + budget envelope**
  from production (inv. 10). A producer never renders its own verdict. *This is
  the keystone — get it wrong and no later policy recovers it.*
- Verdicts carry a **labeled basis** (oracle-verified / concordant-under-test /
  contested), never bare "true."
- A **neutral pool** exists for any penalty/forfeiture flows (never to judge or
  advocate).
- **Standard of proof is a parameter**, set primarily by the user/archetype and
  modulated by `burnrate`.

**Direction (do NOT spec yet — let the built system price it):** the acceptance
gate becomes a *court*, not a one-shot checker — adversarial in method, shared in
telos (the prompt is the charge). Opposed advocates (evidence-for / -against) with
**equal, neutrally-funded** budgets build and cross-examine the strongest case
each way; a judge with no stake in direction rules on a *contested record* and on
whether each side was argued at strength; "contested" becomes an *earned verdict
of due process*. Two checkpoints: **plan pre-registration** (catch a gerrymandered
ACS before it runs — the only check on the root that the root can't evade) and
**result peer-review**. Due process is **stakes-gated** into tiers (summary /
adversarial / full trial) by the same marginal-value discipline. Independence
must be **diversity, not just separation** (different models/methods; physical
reproduction where it counts — IGoR's inter-lab concordance); adjudicator
*disagreement* promotes to contested.

**Producer-funded bond (candidate policy):** the plaintiff (asserting producer)
posts a **proportionate bond** to enter adjudication — forfeit-on-defect to the
neutral pool, **contested and honest-negative are no-fault**, with a subsidy valve
for high-value/high-uncertainty questions so budget never decides which truths get
certified. Separates *the money that deters* (producer may fund) from *the money
that decides* (judge must be neutrally funded, rewarded for calibration). Issuer-
pays-the-rater is the anti-pattern to avoid.

---

## 13. Build sequence (issue-able milestones)

- **M0 — host + contract.** ARM64 container, `/invocations`+`/ping`:8080, A2A;
  instantiate one hardcoded pattern from static ACS; deploy to AgentCore.
- **M1 — work chokepoint.** Gateway model path; metering for Bedrock + local.
- **M2 — governor + acceptance seam.** Reserve/Settle/Release, conservation, WAL,
  lexicographic acceptance→surplus, **grant rate-awareness (amount+period) +
  burnrate default-standard**, and acceptance as a **separate-envelope** node
  (summary judgment).
- **M3 — close the recursion (keystone).** Bootstrap node emits a real multi-node
  ACS the host re-instantiates; planner-as-root + scoping/quote; budget in `ctx`.
  Acceptance test = §14.
- **M4 — second transport.** Placer + A2A session; budget/deadline/cancel + settle
  on the wire.
- **M5 — ledger reconciliation.** Distributed conservation; surplus signal to
  planner/burnrate; four exits end-to-end.
- **M6 — compute synthesis.** ComputeSynthesis → WorkloadSpec → spore.host MCP
  backend (`reckoner.frontier` + provision/run); compute metered through gateway;
  attestation on results.
- **M7+ (additive):** court layer (advocates/tiers/bonds) · burst pool · router
  cascade + Trainium rungs · lagotto · Cedar/LKI gating · overcommit + degradation
  · archetype expansion.

Each milestone independently demonstrable; leaves the spine working.

---

## 14. Acceptance test (the gate)

On *"does microglial TREM2 signaling modulate tau propagation in the entorhinal
cortex, and what's the current evidence?"* the system must:
1. emit a **two-phase composite** graph (evidence-synthesis → mechanistic head);
2. **scope** the entity expansion (TREM2 pathway, "modulate" direction, tau-spread
   contestation, EC as Braak-I origin) without flattening or exploding it;
3. return a **shape, not a verdict** — preserve *contested / stage-dependent*
   rather than manufacturing yes/no;
4. **complete with surplus** and an honest exit, banking margin on an accepted
   (incl. negative/contested) result.

Failing any one tells you which component lied.

---

## 15. Open forks — empirical, for the built system

1. **Naming + org.** Telos confirmed; org placement (own repo vs spore-host peer);
   name-availability sweep (npm/PyPI/crates/GH/domain) before commit.
2. **Burn-rate landing policy.** How aggressively `burnrate` modulates the default
   standard of proof to land the grant near-zero — the thermostat curve.
3. **Scoping-pass depth.** How much the quote may spend to be trustworthy
   (accuracy vs cost; padding-risk vs purpose-defeat). Exploration-depth gate at
   the front door.
4. **Standard-of-proof inputs.** Set by prompt, inferred from archetype, or
   adjudicated — and how question-stakes vs grant-burn combine.
5. **Surplus strength.** Strict satisfice at the floor vs stop at the knee
   (leaning knee — reuses the four-exit rule).
6. **ComputeSynthesis: planned vs emergent** (IGoR-favored: emergent).
7. **Court policy (all of §12 "direction"):** advocate assignment (fixed vs bid);
   judge remand; appeal path + who pays a frivolous one; bond curve width — the
   load-bearing empirical question (filter junk without letting budget decide
   truth); disagreement→contested auto-promotion; independence source (different
   models vs physical reproduction).
8. **Burst pool:** conservative vs overcommit.
9. **Gateway gate:** synchronous vs eventually-consistent.
10. **ACS storage:** content-addressed + versioned (q0-layer style) so re-plan is
    a diff?

---

## Hand-off note (for Claude Code)

Build order is M0→M3 first; nothing downstream is believable until M3 passes §14.
Two disciplines that must hold from the first commit, because they're unrecoverable
later: **(a)** acceptance is a separate-envelope node, never folded into a producer
(inv. 10 / §12 seam); **(b)** the governor is grant-rate-aware (amount+period),
not total-budget-aware (inv. 4 / §9). The first concrete artifact to design is
`bootstrap.acs` — the hand-authored seed the recursion bottoms out on; it quietly
fixes the system's defaults for decomposition and standard of proof before any
question arrives, so it is more opinionated than it looks and deserves explicit
design, not a stub.
