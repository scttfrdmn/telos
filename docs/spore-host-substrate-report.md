# spore.host substrate — source study report

**Status:** study only. No Telos code, no design, no doc edits to `telos-architecture.md`.
Every claim below is from the Go (or Python/Groovy) **source** of the cloned repos,
cited as `repo/path:symbol`. Where I could only infer (not confirm), it is marked
**[INFERRED]**. Read against commit HEADs cloned 2026-06-21.

Repos read (source, not READMEs):
`spore-host/spawn` (390 Go files), `spore-host/truffle` (51), `spore-host/lagotto`
(55), `spore-host/spore-host` (umbrella), `spore-host/cohort` (from M4),
`spore-host/nf-spawn` (Groovy), `scttfrdmn/aws-microbiome-demo` (Python),
`spore-host/spore-host-mcp` (skim).

---

## 1. Package & API surface (library vs CLI)

**There is a first-class, in-process Go library API. The CLI is a thin Cobra shell
over it. A Go caller (Telos) gets real types, not a subprocess.**

- **spawn — the core launch library is `pkg/aws`** (`module github.com/spore-host/spawn`):
  - `spawn/pkg/aws/client.go:Client` with the full lifecycle surface:
    `Launch(ctx, LaunchConfig) (*LaunchResult, error)` (`client.go:288`),
    `Terminate(ctx, region, id)` (`:1185`), `StopInstance(ctx, region, id, hibernate bool)`
    (`:1471`), `StartInstance` (`:1490`), `GetInstanceState` (`:1143`),
    `WaitForRunning(ctx, region, id, timeout)` (`:1161`),
    `ListInstances(ctx, region, stateFilter) ([]InstanceInfo, error)` (`:1289`),
    `UpdateInstanceTags` (`:1203`).
  - Types: `LaunchConfig` (`client.go:113`, ~70 fields), `LaunchResult` (`:239`),
    `InstanceInfo` (`:1257`).
  - **`spawn/pkg/launcher`** is a higher-level wrapper: `launcher.Provision(ctx, *aws.Client, aws.LaunchConfig, Options) (*aws.LaunchResult, error)` (`launcher/provision.go:50`)
    plus `BuildLinuxBootstrap`/`EncodeLinuxUserData` (`launcher/bootstrap.go:75,39`).
    `Provision` is what orchestrates FSx + userdata + launch as one call.
  - Other library packages of interest: `pkg/cost`, `pkg/agent` (the on-node daemon
    lib), `pkg/mpicohort` (see §5), `pkg/orchestrator` (burst/autoscale),
    `pkg/scheduler`, `pkg/staging`, `pkg/storage`, `pkg/provider`.
  - The CLI (`spawn/cmd/*.go`, Cobra) calls these packages; e.g. `cmd/launch.go`.
- **truffle — library `truffle/pkg/aws`** (`module github.com/spore-host/truffle`):
  `Client.SearchInstanceTypes(ctx, regions, matcher, FilterOptions) ([]InstanceTypeResult, error)`
  (`pkg/aws/client.go:251`), `GetSpotPricing(ctx, []InstanceTypeResult, SpotOptions) ([]SpotPriceResult, error)`
  (`:796`), `GetQuotas` (`pkg/quotas/...:120`), `CanLaunch` (`:292`). `pkg/find.ParseQuery`,
  `pkg/find.FindResult` for the query DSL.
- **lagotto — library `lagotto/pkg/watcher`** (`module github.com/spore-host/lagotto`):
  `watcher.Evaluate(*Watch, MatchCandidate) *MatchResult` (`pkg/watcher/evaluate.go`),
  `watcher.Holder`/`NewHolder` (`holder.go:14`), `ClassifyFailure` (`failure.go:74`).
  `pkg/deploy` deploys the watcher as a Lambda.
- **spore-host (umbrella) — NOT a Go module.** No root `go.mod`. It is `web/`
  (dashboard HTML/JS), `lambda/spore-bot` + `lambda/rest-api` (each its own
  `go.mod`), and `sdk/python`. The "ephemeral launcher" branding is a
  front-end/notification/REST layer, **not** a launch library. Telos would not
  import spore-host; it imports spawn/truffle/(lagotto).
- **spore-host-mcp** imports `spawn/pkg/aws`, `truffle/pkg/aws`, `truffle/pkg/find`,
  `truffle/pkg/quotas` directly (`spore-host-mcp` handler.go:10-16) and "mirrors the
  CLI" — **confirms these are the real libraries**; MCP is a thin wrapper. (We are
  not using MCP for Telos.)

**Architectural-smell check (the demo's Python-shells-out pattern):** the microbiome
demo (`aws-microbiome-demo/src/microbiome_demo/spawn.py:_run_spawn` and `truffle.py`)
uses `subprocess.run` against the **CLIs** — because it's Python. A **Go** caller is
NOT reduced to shelling out: it links `spawn/pkg/aws` + `spawn/pkg/launcher` +
`truffle/pkg/aws` in-process. **Confirmed by `spawn/pkg/mpicohort/adapter.go`, which
is exactly a Go-in-process consumer of `pkg/aws`.** No Go-to-Go shelling smell.

---

## 2. The lifecycle model — THE HINGE

**The full lifecycle is enforced by an on-node daemon (`spored`, library `pkg/agent`)
and surfaced to a caller ONLY as side-effects: EC2 tag writes + Slack/file
notifications. There is no push channel, no callback, no event stream to an
in-process Go caller. Observation is by POLLING `ListInstances` (tags + EC2 state).**

The daemon: `spawn/cmd/spored/main.go` → `pkg/agent/agent.go:Agent`, started via
userdata (`pkg/userdata/queue.go:34` "Wait for spored to be installed and running —
downloaded from S3 during boot"). `Agent.Monitor` (`agent.go:266`) runs:
- a **lifecycle ticker** → `checkAndAct(ctx)` (`agent.go:324`), and
- a **separate spot-interruption goroutine** `monitorSpotInterruptions` (`:306`),
  deliberately off the critical path (`:276-282`: a blocking IMDS call must never
  gate TTL/idle enforcement — comment cites #65).

**TTL** — `LaunchConfig.TTL`/`IdleTimeout`/`HibernateOnIdle`/`CostLimit` are set at
launch (`client.go:138-145`) and written as `spawn:*` tags. Enforced on-node in
`checkAndAct`. **INVARIANT (cited #72, `agent.go:~372`): "TTL expiry ALWAYS
terminates"** — no stop/hibernate branch, no `ttl-action` tag honored; TTL is the
unconditional backstop (avoids the #71 "zombie" that stops then re-checks TTL).
`TTLDeadline` is absolute, anchored to launch time, so a spored restart can't reset
it (`agent.go:113-120`, `360-368`). Also enforced **off-node** by a Lambda:
`spawn/lambda/ttl-reaper/main.go` (belt-and-suspenders for instances whose spored
died).

**Idle-detect-and-stop** — yes, self-decided on-node. `checkAndAct` step 4
(`agent.go:~93`): `isIdle()` (CPU via `/proc/stat`, session count, DCV for streaming
instances). On idle-timeout it **stops** (or **hibernates** if `--hibernate-on-idle`,
`agent.go:~103`). **The caller is not told directly** — it learns by polling and
seeing `State` go to `stopped`/`stopping` (`InstanceInfo.State`), or via the Slack
notifier. Idle resets on activity (`lastActivityTime`).

**Cost limit** — `checkAndAct` step 3 (`agent.go:~70`): fires independently of TTL,
"first-to-fire wins". So a node can self-terminate on a dollar ceiling.

**On-complete / pre-stop** — `OnComplete` (terminate|stop|hibernate) triggered by a
`CompletionFile` (default `/tmp/SPAWN_COMPLETE`) (`LaunchConfig:client.go`,
`checkAndAct` step 0). `PreStop` shell hook runs before any lifecycle-triggered
stop, as the tagged `spawn:local-username` (`client.go` PreStop docs, `agent.go:runPreStop`).

**SPOT RECLAMATION** — `monitorSpotInterruptions` polls IMDS every **5 s**
(`agent.go:310`); `checkSpotInterruption` (`agent.go:~1010`) on a notice:
runs `Cleanup`, `warnUsers`, `runPreStop(shortTimeout=true)` (stay in the 2-min
window), Slack `notifier.Notify("spot_interrupt", …)`, and **writes a file
`/tmp/spawn-spot-interruption.json`** (`agent.go:1053`). The code comment
(`agent.go:1066-1067`) is explicit: *"Future enhancement: Support webhooks, email,
SNS… For now, the notification file can be picked up by external monitoring."*
**→ Spot reclamation is NOT surfaced to a programmatic caller today.** A caller sees
it only as: the instance disappearing (state→`terminated`), a tag change, a Slack
message, or that on-node file (not visible off-node).

**Answer to the hinge question:** lifecycle events are **PUSHED on-node only**
(daemon acts locally); to an off-node caller they are **POLLED side-effects** —
EC2 `State` transitions and `spawn:*` tags via `ListInstances`. The only "push" off
the node is **Slack/SMS/Discord via `spore-host/lambda/spore-bot`** (human channel),
not a machine API. **This single fact shapes Telos's whole compute fault/cost model:
the compute substrate's Observer is a POLLER (exactly like the M4 AgentCore Observer
and the `mpicohort` Observer), and spot reclamation must be inferred from
state/tags, not awaited on a channel.** [INFERRED that no other event path exists:
I grepped `pkg/agent`, `pkg/observability`; found only tags + notifier + file.]

---

## 3. Cost observability

**Cost is COMPUTED from elapsed-wall-clock × a static rate table, not read from AWS
billing. Two separate cost surfaces exist:**

- **spawn `pkg/cost`** (`cost/cost.go`): `Client.GetCostBreakdown` over a DynamoDB
  state history (`SweepRecord`, `StateTransition`, `EBSVolume`). Compute cost =
  `calculateComputeCost(inst, runningHrs)` = `runningHrs * pricing.GetEC2HourlyRate(region, type)`
  (`cost.go:257-258`), where `pricing` is the **static** `github.com/spore-host/libs/pricing`
  table (`cost.go:13`). Storage = `pricing.GetEBSMonthlyRate`; network =
  `IPv4Count * pricing.GetIPv4HourlyRate`. Field name is `EstimatedCost`
  (`RegionalCost`, `InstanceTypeCost`). Running hours come from recorded state
  transitions, with "no history → total elapsed" fallback (`cost.go:289`).
  - The **on-node agent also writes cost tags**: `spawn:compute-seconds`
    (`agent.go:580`) and `spawn:ebs-hourly-cost` (`agent.go:138` comment) — so a
    poller can read accrued compute-seconds off a tag.
- **truffle** surfaces **forward-looking price**, not accrued cost:
  `SpotPriceResult{SpotPrice, OnDemandPrice, SavingsPercent, AZ, Timestamp, ProductType}`
  (`truffle/pkg/aws/client.go:62`) from `GetSpotPricing` (real AWS Spot pricing API),
  and `InstanceTypeResult.OnDemandPrice` (`:41`). Granularity: per-(instanceType,
  region, AZ), with a timestamp. This is the "frontier" input — current spot $/hr
  per AZ with savings vs on-demand.

**For Telos:** there is **no running/projected $ from a live `Launch` handle** —
`LaunchResult` carries no cost. A consumer computes cost externally from
`launch-time + rate-table` (truffle for the rate, elapsed for the hours), or reads
the `spawn:compute-seconds`/`spawn:ebs-hourly-cost` tags by polling. This matches
the microbiome demo's `pipeline.py` doing
`ec2_cost = (elapsed/3600) * (head_price + queue_size*task_price*0.4)` from a
hardcoded `_INSTANCE_PRICES` dict (`pipeline.py:106-108`). **Cost is estimate, not
billed truth.**

---

## 4. Data & storage lifecycle — the demo's "lie", traced

**Finding that DIVERGES from the prompt's stated example:** the prompt said the real
microbiome path "stages and pre-processes data into S3-backed FSx Lustre." **In the
current source of `scttfrdmn/aws-microbiome-demo` there is NO FSx Lustre at all** —
`grep -rniE 'fsx|lustre'` over the whole repo returns **zero** matches. So the
specific FSx claim does not hold for this repo as cloned; I report what the source
actually does.

**The README claim IS still false, but by a different mechanism (S3, not FSx):**
- README (`README.md:36`): *"Reads its SRA file directly from RODA
  (`s3://sra-pub-run-odp/`) — no staging, no copying, $0 data cost."*
- Actual path (`src/microbiome_demo/worker_script.py`, `run_headless.py`,
  `nextflow_config.py`):
  1. **Control-plane staging to S3 IS done**: `run_headless.py:121-127` uploads the
     SRR list, `nextflow.config`, and `main.nf` to `s3://bucket/…`
     (`worker_script.upload_nextflow_config`, `upload_main_nf`); `spawn.py:_upload_script`
     uploads task scripts so `--command` can fetch them. → not "no copying".
  2. **Per-task data**: each sample instance does `aws s3 cp s3://sra-pub-run-odp/sra/$SRR/$SRR`
     from RODA (`--no-sign-request`, same region us-east-1 → no egress)
     (`worker_script.py:83`), `fasterq-dump` to **local disk**, then writes outputs
     and the Nextflow **work-dir to S3**: `-w s3://${BUCKET}/work/${JOB_NAME}/fetch/`
     (`worker_script.py:329`), results to `s3://${BUCKET}/results/${JOB_NAME}`
     (`:155`). → there IS S3 staging + persistent S3 storage cost.
  3. The Kraken2 DB (11.9 GB) is **pre-staged on the AMI/EBS** (`build_ami.py:14`,
     `nextflow_config.py:105`) — a baked-in cost, not per-run.
- **So the truthful cost reality:** RODA reads are genuinely ~$0 (no-sign, in-region),
  but the demo (a) stages config/scripts to S3, (b) persists a Nextflow work-dir AND
  results to S3 (hourly-billed storage until torn down), and (c) carries an AMI with
  an 11.9 GB DB. `teardown.py` exists to clean up; **[INFERRED]** S3 work/results
  persist until teardown is run (per-run buckets, not reused — the prefix is
  `${JOB_NAME}`-scoped).

**Separately-metered compute vs persistent hourly-billed resources:** in *this*
demo, the only persistent hourly resource is **S3 storage** (work + results
prefixes) + the AMI's EBS snapshot; the compute is per-task EC2 (metered by
elapsed). **There is no FSx "perishable rectangle" in this demo.** HOWEVER —
**spawn itself fully supports the FSx-Lustre perishable-rectangle pattern**, just not
exercised by this demo: `LaunchConfig.FSxLustreCreate` + **`FSxLifecycle` ("ephemeral"
| "durable", REQUIRED, cited #193) + `FSxTTL`** (`client.go` FSx fields), provisioned
by `launcher.Provision` (test `provision_test.go:TestProvision_FSxLifecycleFailClosed`)
and `pkg/aws/fsx.go` (`CreateFSxLustreFilesystem`, `DeleteFSxFilesystem`,
`RecallFSxFilesystem`), with import/export to S3 (`FSxImportPath`/`FSxExportPath`)
and an on-node mount by spored (`pkg/agent/fsx_mount.go`, `spawn:fsx-pending` tag).
So the perishable FSx rectangle is a **spawn primitive with explicit lifecycle**, and
`telos-architecture.md §8`'s cost concern is right in general — it's just that the
microbiome demo isn't the example of it (it uses S3, not FSx).

**Note vs §8 (do not fix the doc):** `telos-architecture.md §8` should eventually
reflect that (a) FSx ephemeral/durable lifecycle is a first-class spawn input with a
required explicit lifetime, and (b) the microbiome demo does NOT use FSx — any §8
text leaning on "the demo stages to FSx" is inaccurate to current source. Flagged,
not changed.

---

## 5. cohort fit

**It fits — and the fit is already BUILT in spawn, not hypothetical.**
`spawn/pkg/mpicohort/adapter.go` is a complete cohort provider over `spawn/pkg/aws`:

- **`Actuator`** (`adapter.go:30`): `Launch` maps `cohort.EntityIntent` →
  `aws.LaunchConfig` — sets `cfg.Name = intent.ID`, `cfg.ClientToken =
  intent.IdempotencyToken` (comment: "deterministic — safe to re-issue"), and reads
  `intent.Placement.(cohort.RungPlacement)` for `InstanceType`/`AZ`/`Spot`
  (`adapter.go:38-44`) — i.e. it consumes the **v0.2.0 Placement seam** Telos's M4
  review earned. `Start`/`Stop(StopHibernate)`/`Terminate` map to the `aws.Client`
  ops; `providerID` resolves an `EntityID` → instance by the `Name` tag.
- **`Observer`** (`adapter.go:~104`): `Observe` POLLS `ListInstances`, indexes by
  `Name`, maps EC2 `State` → `cohort.LifecycleState` via `mapState` (`adapter.go:bottom`:
  pending→Launching, running→Running, stopping/stopped→Stopped,
  shutting-down/terminated→Failed), returns `StateUnknown` for an unseen id (lag,
  not absence — cohort's rule). **Same poll-and-map shape as Telos's M4 AgentCore
  Observer.**
- **`Classifier`** (`adapter.go:~`): maps `aws.LaunchError.Code` →
  `InsufficientInstanceCapacity`/`InsufficientHostCapacity`/`MaxSpotInstanceCountExceeded`/
  `SpotMaxPriceTooLow` → `FaultCapacityExhausted`; `RequestLimitExceeded`/`Throttling`
  → `FaultThrottle`; else `FaultTerminal`. **This is the real ICE→capacity-exhausted
  mapping that drives cohort's rung fallback.**
- `Enroller` is a stub (`Operational: true`) — MPI readiness is domain work;
  `Assembler.WireUp` is the PMIx hook.

**Where it could fight the seam (one spot):** the **on-node lifecycle (spored) is
invisible to cohort's Observer.** cohort's phase model is *bring-up* oriented
(LaunchAcked → Running → Enrolled → Barrier → Assembly → Ready); spawn's idle-stop /
TTL-terminate / spot-reclaim / cost-limit are *post-Ready, autonomous teardown*
events the Observer can only see as a later `State` change (running→stopped/
terminated). cohort has no "the entity self-retired" phase — a self-stopped idle
instance reads as `StateStopped`, indistinguishable from a deliberate Stop. **This is
not a seam break (the Observer still reports truthfully), but it means a Telos
compute substrate must interpret a post-Ready state change against the `spawn:*`
tags (TTL deadline, idle, spot file) to know WHY** — and that "why" is only legibly
available on-node. **[Confirmed gap, not break.]** If Telos needs the reclamation
*reason* programmatically, that's the upstream conversation §2 flagged (spawn surface
the spot signal as more than a local file), not a Telos workaround.

---

## 6. The grain — what spore.host WANTS to be

cohort's grain is "entities as named identities." spore.host's grain, from source:

**spawn wants to be a *fire-and-forget autonomous instance with a baked-in
self-destruct contract*.** The whole design centers on the instance taking care of
its own death: you encode the *policy* at launch (TTL, idle-timeout, cost-limit,
on-complete, pre-stop, spot behavior) as **tags + an on-node daemon (spored)**, and
then you let go. The caller is not expected to babysit a lifecycle — the node
enforces its own contract and the world finds out by reading tags or getting a Slack
ping. This is elegant and constraining in the same move:
- **Elegant:** a launch is a complete, self-terminating unit; nothing leaks if the
  caller crashes (the ttl-reaper Lambda + on-node TTL are belt-and-suspenders, cited
  #72). The idempotency token (deterministic in cluster/entity/generation, #108) is
  *already* there for exactly cohort-style callers.
- **Constraining:** observability is **eventually-consistent, tag-and-poll**, never
  an event stream. There is no synchronous "tell me the moment this goes idle." A
  consumer that needs tight, pushed lifecycle state must build a poller and treat
  tags as the source of truth. **This is the grain Telos must design WITH:** the
  compute substrate is a poller over a self-governing fleet, not a controller of
  externally-driven instances.

**truffle's grain:** read-only *discovery / frontier* — "what can I launch, where,
at what spot price, within what quota." Pure queries (`SearchInstanceTypes`,
`GetSpotPricing`, `GetQuotas`, `CanLaunch`), no mutation. It is the
reckoner-frontier input, cleanly separated from spawn's actuation.

**lagotto's grain:** *capacity is only knowable by trying* — "launch IS the capacity
test (no read-only API reports this)" (`watcher/failure.go:19`). lagotto is a
retry-until-capacity-appears poller that holds a 30-min reservation
(`holder.go:24`) and classifies failures into retry-vs-stop. It's a patience
primitive for scarce (GPU/capacity-block) launches.

**spore-host umbrella's grain:** the *human/ops surface* — dashboard, REST, and a
notification bus (spore-bot → Slack/Discord/Teams/SMS). Not a compute primitive.

**Surprising/elegant, that a README wouldn't reveal:** (1) the **#72 TTL-always-
terminates invariant** is a hard-won correctness rule (the #71 zombie) baked into the
agent, not a config knob — TTL cannot be softened to "stop". (2) The spot monitor is
*deliberately* decoupled from the lifecycle ticker (#65) so a stalled IMDS call can't
silently disable TTL enforcement — a real production scar. (3) The idempotency token
and Placement consumption mean **spawn's authors already think of cohort as a
first-class consumer** — Telos is walking a path spawn anticipated.

---

## 7. Primitives vs compositions

**PRIMITIVES — Telos should build on these:**
- `spawn/pkg/aws.Client` — Launch / Terminate / Stop(hibernate) / Start /
  GetInstanceState / WaitForRunning / ListInstances / UpdateInstanceTags. The
  lifecycle actuator + observer.
- `spawn/pkg/launcher.Provision` + `BuildLinuxBootstrap`/`EncodeLinuxUserData` — the
  one-call launch that wires userdata + FSx + spored.
- `spawn/pkg/aws` FSx ops (`CreateFSxLustreFilesystem`/`Delete`/`Recall`) +
  `LaunchConfig.FSxLifecycle`/`FSxTTL` — the explicit perishable-rectangle primitive.
- The **`LaunchConfig` tag-contract fields** (TTL/IdleTimeout/HibernateOnIdle/
  CostLimit/OnComplete/PreStop/ClientToken) — the self-destruct contract is a
  primitive you set, not code you write.
- `spawn/pkg/agent` (spored) — the on-node enforcer; Telos consumes its *effects*
  (tags/state), it does not reimplement it.
- `truffle/pkg/aws` — SearchInstanceTypes / GetSpotPricing / GetQuotas / CanLaunch.
  Discovery + spot frontier.
- `spawn/pkg/mpicohort` — **the reference cohort provider**; Telos's compute
  substrate is a generalization of this (1-cohort for a one-shot job; collective for
  an all-or-nothing sweep), not a new invention.
- `lagotto/pkg/watcher` — optional patience primitive for scarce-capacity launches.

**COMPOSITIONS — optional, or to ignore:**
- **nf-spawn** (Groovy Nextflow plugin: `SpawnExecutor`/`SpawnTaskHandler`) — the
  right tool ONLY when the workload IS a Nextflow pipeline. Telos runs much that is
  not (one-shot model run, parameter sweep, 9-min GPU job); **ignore as a template.**
- **aws-microbiome-demo** (Python, shells out to CLIs) — a worked example, not an
  API path; its data/cost shape is demo-specific. Reference only.
- **spore-host-mcp** — a CLI-mirroring MCP wrapper; not Telos's path.
- **spore-host umbrella** (web/REST/spore-bot) — ops/human surface; not a primitive.
- `spawn/cmd/*` (the CLI), `spawn/pkg/orchestrator` (burst/autoscale policy),
  `spawn/pkg/scheduler`, `pkg/queue`, `pkg/sweep` — opinionated compositions over the
  primitives; Telos may borrow ideas but should not depend on them as the substrate.

---

## Bottom line

**The AWS compute substrate is roughly ONE milestone of Telos-side work, NOT a
rewrite — but it depends on an honest acceptance of spore.host's grain, and there is
one upstream conversation worth having.** spawn already exposes a real in-process Go
library (`pkg/aws` + `pkg/launcher`) and *already ships a working cohort provider*
(`pkg/mpicohort`), so Telos's compute substrate is a generalization of an existing,
proven adapter — the same poll-and-map Observer / EC2-error Classifier shape as the
M4 AgentCore substrate, now over real instances with FSx-rectangle lifecycle. **The
single biggest risk is the §2 hinge: lifecycle events (idle-stop, TTL-terminate, and
especially SPOT RECLAMATION) are surfaced to a programmatic caller ONLY as polled
side-effects — EC2 state + `spawn:*` tags + an on-node file — never pushed.** Telos's
compute cost/fault model must therefore be poll-and-infer (a reclaimed spot instance
looks like a `terminated` state plus a tag, not an awaited event), and the reason for
a self-retirement is only legibly available on-node. If Telos needs the reclamation
*reason* pushed off-node, that is a coordinated spawn/cohort upstream ask (surface
the spot signal beyond a local file), exactly the discipline of the cohort#1 Rung fix
— not a Telos workaround. Secondary risk: §8 of the architecture doc describes the
data/cost path partly from the demo's (false) README; the FSx perishable-rectangle is
real in spawn but absent from the demo, and cost everywhere is *estimated* (elapsed ×
static rate table), not billed — Telos's gateway metering for compute must own that
estimate explicitly, as the M1 synthesized-cost discipline already anticipated.
