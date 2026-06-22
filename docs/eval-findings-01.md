# Eval findings 01 — the M0–M6 spine on real, untuned questions

**What this is.** A measured run of the working spine against a real Bedrock
backend, on 10 untuned research questions spanning five archetypes and seven
domains, plus one real-EC2 orphan-reconciler test. Every claim below cites
captured run data (`docs/eval/phase1-run.jsonl`, `docs/eval/phase2-report.json`)
or an independent AWS check. Where I infer rather than measure, it is marked
**[inferred]**. The harness (`cmd/telos-eval`, `cmd/telos-eval-compute`) is
permanent infrastructure — every M7 piece is validated against it.

**Account/region/cost.** Account 942542972736, us-west-2, profile `aws`. Phase 1
real metered spend: **< $0.01** (Haiku/Sonnet, 10 questions). Phase 2: one
t4g.small spot instance for ~seconds ≈ **negligible**. Both far under their caps
($5 / $2). `ports.go` UNCHANGED — Telos imports cohort@v0.2.0 read-only; the
sporehost substrate fills the provider seam without touching the core.

**Discipline note.** Nothing was tuned to pass. The findings below are mostly
*failures of generalization*, recorded faithfully — that is the value. Two harness
bugs were fixed mid-session (HTTP-status capture; envelope-vs-real-spend cap split)
because they were *measurement* defects, not system behavior; no Telos spine code
was changed to flatter a result.

---

## What it took to get a real call at all (two pre-findings)

Before any of the four questions could be measured, two real blockers surfaced —
both are findings in their own right:

1. **The seed's placeholder budget blocks every real grant smaller than $100.**
   First real run: HTTP 500, `conservation breach: requested 100.000000 but only
   0.500000 remains`. The planner emits a graph budgeted from `bootstrap.acs`'s
   **placeholder $100** (`spec.Budget`), not from the actual run envelope. The run
   envelope never reaches the planner (`host/planning_agent.go` uses
   `b.spec.Budget`). So a real $0.50 grant fails closed before spending a cent.
   *Measured*, `phase1-run.jsonl` (first run, all 500). **The grant-rate governor
   correctly fails closed — but the budget doesn't propagate from grant → emitted
   graph.** Worked around in the eval by setting the reservation envelope to $200;
   the real-dollar cap is enforced separately on metered spend.

2. **Bedrock Sonnet/Haiku 4.5 require an inference-profile ID, not the bare model
   id.** Second run: `ValidationException: Invocation of model ID
   anthropic.claude-sonnet-4-5-… with on-demand throughput isn't supported. Retry
   with … an inference profile.` The fix is the `us.` prefix
   (`us.anthropic.claude-sonnet-4-5-20250929-v1:0`). *Measured.* Not a Telos bug —
   an environment/config fact — but `host.NewDeps` takes a bare model id with no
   guidance, so a first-time real run fails opaquely. [inferred] worth a doc note
   or a profile-resolution helper.

---

## Q1 — How often does the cheap path suffice?

**Not measurable as designed, because of a deeper finding: the model's output is
decorative.** The verdict the system returns is computed entirely from
*fabricated, deterministic* provenance, not from the model.

What I measured:
- A **direct** `gateway.Invoke` to real Bedrock Haiku meters correctly:
  `$0.000231, tokens 26/41, root grant 200 → 199.999769` (probe). So the gateway
  cost path works per-call.
- But in a full run, **every question reports `cost_total=0`, `tier=""`,
  `backend=""`, `reservoir_after=200` (untouched)** (`phase1-run.jsonl`, all 10).
  The model IS called and paid for (the 400→200 transition proves real calls), but
  the spend settles against **nested child grants** created by the recursion
  (`planning_agent.reserveChild`), and **nothing aggregates those to the run root**
  the harness (or any consumer) reads. Run-level cost is unobservable.
- The final output is the **acceptance marker**
  (`"[acceptance: accepted — contested] …"`), never the model's answer
  (`phase1-run.jsonl` `output_excerpt`, all 10). The verdict (`accepted`,
  `concordant`/`contested`) is derived from `evidenceSources()` — hardcoded
  stand-in provenance (2 support + 2 dispute for mechanistic) in
  `host/research_agents.go` — **not** from the model's content.

**Finding:** the cheap-vs-escalation question can't be answered yet because the
spine never escalates *on the basis of the model's work* — escalation isn't wired
to anything the model produces, and cost isn't visible at the run level. The
empirical bet the architecture rests on (expensive adjudication is rare) is
**untestable on the current spine**: the adjudication is mechanical and free, and
the expensive model call is decorative. This is the single most important finding.

---

## Q2 — Does scope calibration generalize past TREM2? **No. Measured, decisively.**

The M3 report flagged that §14's scope check passed on a TREM2-*tuned* detector and
warned it might not generalize. It does not. On all 10 untuned questions:

- **10/10 under-fan** (`scope_within=false`, every one carries
  `WARNING: expansion is flat (under-fanned)`).
- The emitted "entities" are **degenerate keyword artifacts** — `detectSubject`
  grabs the first non-stopword token and appends "pathway":
  | question | emitted scope entities |
  |---|---|
  | mat-boron-creep | `["grain-boundary pathway"]` |
  | german-minwage-employment | `["2015 pathway"]` |
  | tokamak-stellarator | `["compare pathway"]` |
  | technosignatures | `["most pathway"]` |
  | ceria-alumina-stability | `["adding pathway"]` |

  (`phase1-run.jsonl`, `scope_entities`.) "2015 pathway", "compare pathway", "most
  pathway" are not entities — they are the offline regex grabbing a stopword-adjacent
  token. The scope node carries **nothing**; the real model is **never consulted**
  for scope (scope is a pure offline keyword pass in `domain/research/scope.go`).

**The distinction the prompt asked for:** this is unambiguously *"the architecture
did NOT structure the scope"* — there is no model in the scope path to have carried
it. The §14 scope check was a TREM2-shaped unit test; on real input the mechanism
produces noise. **Scope calibration must be model-driven to exist at all.**

---

## Q3 — Where do real questions land on the standard-of-proof / burn-rate curve?

**All 10 hit `standard=concordant`; burn-rate's curve was never exercised.**

- Every question: `standard=concordant` (`phase1-run.jsonl`, all 10). Burn-rate's
  reservoir-over-clock thermostat (`burnrate.DefaultStandard`) never moved the
  default, because **the run envelope ($200 reservation) is enormous relative to
  the sub-cent real spend, and the clock is a fixed 24h with elapsed≈0** — so pace
  is always "way ahead", which would yield `oracle`, yet the seed fallback
  `concordant` is what surfaced. [inferred] burn-rate's signal isn't wired into the
  emitted graph's standard at all in this path — the standard is the seed default,
  not a burn-rate output. The thermostat is **untested against a real
  reservoir+clock** because (a) real spend is invisible (Q1) and (b) elapsed time
  isn't tracked within a run (a known M2 simplification: `Clock{Elapsed:0}`).
- **Finding:** burn-rate cannot land a grant near-zero if it can't see realized
  spend or elapsed time. Both are missing from the live path. Not a curve-shape
  problem (the §15 fork) — a *plumbing* problem: the thermostat has no live signal.

---

## Q4 — Does the disposition race bite? **Not observed; the orphan net holds.**

- **No disposition race observed** in Phase 1 (model-only; no compute lifecycle to
  race) — expected, the race needs real compute timing.
- **Phase 2 exercised the real-compute path and the orphan reconciler against a
  real billable instance**, the single most important real-money safety check:
  - Launched `i-0a21e989541abd9d6` (t4g.small spot, 15m TTL + $0.50 cost-limit
    backstops, FSx none).
  - **Stranded** it (discarded the ledger — simulated a Telos crash).
  - `Launcher.Reconcile(nil)` **detected it as an orphan**, terminated it.
  - Residual check: **0 telos-tagged instances running.**
  - **Independently verified via AWS directly:** `describe-instances
    i-0a21e989541abd9d6 → terminated`; region sweep for `tag-key=telos:entity` +
    `state=running,pending` → **empty.** (`phase2-report.json`,
    `phase2-stderr.log`, plus the direct AWS query.)
- **Finding:** the M6 orphan reconciler — proven against fakes in M6 — **holds
  against real money.** A crash mid-run leaves nothing billing unaccounted. The
  ClientToken-idempotency + tag-list-and-match design works on a live instance.
  The disposition-race concern (`disposition.go`) remains *plausible but
  unobserved*; crucially, **even if a disposition were mislabeled, the orphan
  reconciler is the backstop that prevents lost money** — and that backstop is now
  verified real. Mislabeled disposition ≠ lost money: confirmed.

---

## Distributions (captured)

- **Archetype detection: 3/10 correct** (`cbam-leakage`✓, `satfat-cvd`✓,
  `rome-economic-collapse`✓). 8/10 classified `evidence-synthesis`, 2/10
  `mechanistic`, **0 composite, 0 comparative, 0 quantitative, 0 exploratory** —
  the offline classifier only fires on "modulate/cause" verbs + explicit "evidence"
  markers; real phrasings ("does X improve Y", "did X reduce Y", "compare A vs B",
  "reproduce the exponent") all fall through to evidence-synthesis. **Both
  non-TREM2 composites (mat-boron, ceria-alumina) MISSED → composite detection does
  not generalize.** **Comparative/quantitative/exploratory archetypes are never
  emitted** — the research pack has no shape for them, or the classifier can't
  route to them. (`phase1-run.jsonl`.)
- **Acceptance: 10/10 accepted; basis 8 concordant-under-test, 2 contested.** Zero
  negatives, zero rejections. But this is mechanical (fabricated provenance, Q1),
  so it measures the scaffolding, not epistemic behavior. The two "contested" are
  exactly the two classified mechanistic (which assemble for+against stub sources);
  the genuinely-contested questions classified as evidence-synthesis (satfat was
  caught, but german-minwage and rome were not contested-routed) got a plain
  concordant accept. **Direction-neutrality/contested-preservation can't be
  credited — it's an artifact of the stub provenance, not the model judging.**
- **Surplus:** `banked_surplus=0` everywhere (no run-level settlement reaches the
  root; Q1). Cannot size a burst pool from this. **[inferred]** burst-pool sizing
  needs Q1 fixed first.
- **Where money leaks:** nowhere measurable yet — real spend is sub-cent AND
  invisible at the run level. The leak, when it matters, will be **escalation to
  the frontier model**, but escalation isn't wired (Q1), so there's no leak to
  measure. Router-cascade priority can't be set from this data.

---

## Bottom line — the M7 ordering the data dictates

**The headline: the additive M7 features (court, burst pool, router cascade,
Cedar/LKI, archetype expansion) are almost all premature, because the spine has
three integration gaps that make their value unmeasurable — and in the court's
case, the data says it would fire on noise.** The eval did exactly its job: it
stopped us from building M7 on top of a spine whose model isn't doing the work.

What the data orders, highest return first:

1. **Wire the model into the epistemic path (NOT an M7 feature — a spine
   integration fix, but it gates everything M7).** Today the model is called and
   paid for, but scope, archetype, provenance, and verdict are all computed by
   offline scaffolding; the model's output is discarded. Until the model's content
   drives scope, classification, and the acceptance record, *every* M7 measurement
   is meaningless. **This is the prerequisite, and the eval's central finding.**

2. **Run-level cost/standard aggregation (spine fix).** Nested child-grant spend
   never rolls up to the root; burn-rate has no realized-spend or elapsed-time
   signal. Until this exists, Q1 (cheap-path frequency), surplus, burst-pool
   sizing, and router-cascade priority are all unmeasurable. Second prerequisite.

3. **Model-driven scope + archetype classification (this IS "archetype expansion",
   and it's the highest-value *named* M7 item).** Measured 3/10 archetype, 10/10
   scope under-fan. Comparative/quantitative/exploratory are never even emitted.
   This is where the system most visibly fails real questions, and it's a
   precondition for the composite/mechanistic structure §14 depends on.

4. **THE COURT: defer it — the data says it would fire on noise, and rarely.**
   This is the finding that "saves us from building it wrong." With the model out
   of the verdict path, acceptance is mechanical: 10/10 accepted on fabricated
   provenance, "contested" only when stub for/against sources are assembled. A
   court built now would adjudicate *synthesized* records, not real evidence —
   adversarial theater over fabricated provenance. **The court is only worth
   building after #1 (the model actually produces the evidence the court would
   weigh), and even then the cheap-path-frequency question (#Q1) must first show
   that expensive adjudication is rare. We cannot yet show that. Do not build the
   court.**

5. **Burst pool, router cascade, Cedar/LKI: defer — blocked on #2.** All three are
   tuned from cost/surplus/escalation data that does not yet exist at the run
   level. Sizing a burst pool, prioritizing a router cascade, or scoping
   tool-gating policy on the current measurements would be guesswork. Revisit once
   #2 makes spend observable.

**The one thing verified production-ready:** the **real-money safety net** (orphan
reconciliation, grant-rate fail-closed, TTL/cost-limit backstops). Phase 2 proved a
stranded real instance is detected and terminated with zero residual billing,
confirmed against AWS directly. The budget *spine* is trustworthy with real money;
it is the *epistemic* path (model → scope → verdict) that is hollow.

**Recommendation:** before any M7 feature, close the three spine gaps (#1–#3, in
that order). They are not new milestones — they are the wiring that makes the M0–M6
spine actually use the capable model it already pays for. Then re-run this harness:
if Q1 then shows expensive adjudication is rare, build the court; if it shows
frequent escalation, the router cascade jumps the queue. **The harness is built and
will answer those questions the moment the model is in the loop.** §15 forks remain
open — the data informs them but resolves none.
