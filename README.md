# Telos

> A research question synthesizes a budgeted agent graph that investigates it
> autonomously over heterogeneous runtimes — stopping intelligently, completing
> with surplus, returning honest results including negatives, and spending a real
> grant across its full period rather than to a wall.

*Telos: the system is named for its load-bearing invariant — the original
question is the fixed purpose everything serves and nothing is allowed to
capture. Adversarial in method, shared in telos.*

The user writes a **question**, not a structure. A planner infers the shape of
the inquiry and emits a composition spec (an **ACS** — agenkit patterns stitched
into a graph). A generic host instantiates patterns from it; the graph is bound
to models and a conserved budget, placed on the cheapest runtime that satisfies
its constraints, and metered through one chokepoint.

See [`telos-architecture.md`](telos-architecture.md) for the full design — it is
the source of truth.

## Status

Early. Build sequence is milestone-driven (`M0`→`M7+`) and tracked entirely in
[GitHub Issues / Milestones](https://github.com/scttfrdmn/telos/milestones) — not
in status markdown.

**M0 — host + contract** (current): a generic agenkit host that answers the
AgentCore contract (`GET /ping`, `POST /invocations` on `:8080`) and instantiates
an agent graph from a hand-authored seed ([`bootstrap.acs`](bootstrap.acs)) using
the four base patterns: Sequential, Parallel, Supervisor, React.

## Two disciplines held from the first commit

These are unrecoverable if gotten wrong, so they are enforced at the package
boundary from day one (architecture invariants 10 and 4):

1. **Acceptance is a separate-envelope node, never folded into a producer.** A
   node never settles its own acceptance. The `acceptance` package contains no
   result-producing code; `acs` validation rejects an acceptance node sharing a
   producer's trust envelope.
2. **The budget is a grant — amount *and* period, jointly conserved** — never a
   total. `acs.Budget` carries both axes; nothing expresses "budget remaining"
   without its clock.

## Run locally

```sh
make host          # build bin/telosd
make run           # run on 0.0.0.0:8080
# in another shell:
curl localhost:8080/ping
curl -X POST localhost:8080/invocations -d '{"prompt":"hello"}'
```

ARM64 container (build only — AgentCore deploy is a separate, gated step):

```sh
make docker-arm64
```

## Dependencies

- [`github.com/scttfrdmn/agenkit-go`](https://github.com/scttfrdmn/agenkit-go) —
  agent patterns and the core `Agent` interface (using the distribution mirror;
  see [scttfrdmn/agenkit#660](https://github.com/scttfrdmn/agenkit/issues/660)).
- `spore-host/cohort` — the reconciler. **Not yet published**; the in-process
  substrate uses a local placeholder interface until it lands.

## License

Apache 2.0 — see [LICENSE](LICENSE).
