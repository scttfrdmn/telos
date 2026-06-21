#!/usr/bin/env bash
# Local acceptance check for the M0 host (issue #8/#9).
#
# Boots telosd, asserts GET /ping is healthy and POST /invocations instantiates
# the seed graph (all four patterns + a separate-envelope acceptance node), then
# shuts it down. No model calls, no network egress, no Docker required.
#
# Usage: scripts/local-acceptance.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ADDR="127.0.0.1:18080"
BASE="http://${ADDR}"

cd "$ROOT"
echo "==> building telosd"
go build -o bin/telosd ./cmd/telosd

echo "==> starting host on ${ADDR}"
TELOS_ADDR="$ADDR" ./bin/telosd >/tmp/telosd.acceptance.log 2>&1 &
PID=$!
trap 'kill "$PID" 2>/dev/null || true' EXIT

# Wait for readiness.
for _ in $(seq 1 50); do
  if curl -fsS "${BASE}/ping" >/dev/null 2>&1; then break; fi
  sleep 0.1
done

fail() { echo "FAIL: $1" >&2; exit 1; }

echo "==> GET /ping"
PING="$(curl -fsS "${BASE}/ping")"
echo "$PING" | grep -q '"status":"healthy"' || fail "ping not healthy: $PING"
echo "$PING" | grep -q '"seed_hash":"sha256:' || fail "ping missing seed hash: $PING"

# M3: the seed is a planning-root base case that CLOSES THE RECURSION — it reads
# the question, the planner emits a real graph, and the host re-instantiates it.
# We exercise the §14 STRUCTURE offline on echo (the real-model QUALITY is the
# creds-gated live gate, TestSmoke_TREM2_Section14). The TREM2 string is used so
# scoping has real entities to bound.
echo "==> POST /invocations (TREM2 — closes the recursion)"
INV="$(curl -fsS -X POST "${BASE}/invocations" \
  -H 'content-type: application/json' \
  -d '{"prompt":"does microglial TREM2 signaling modulate tau propagation in the entorhinal cortex, and what'"'"'s the current evidence?"}')"

# The seed itself is the minimal planning base case (1 node).
echo "$INV" | grep -q '"node_count":1' || fail "seed should be the 1-node planning base case: $INV"

echo "$INV" | python3 -c '
import sys, json
r = json.load(sys.stdin)
md = r.get("metadata", {})

# §14 #1 — recursion closed to a two-phase COMPOSITE graph.
if r.get("archetype") != "composite":
    sys.exit("§14 #1: archetype = %r, want composite" % r.get("archetype"))

# §14 #2 — scoping bounded between flatten and explode, inspectable.
sc = md.get("telos.scoping")
if not sc:
    sys.exit("§14 #2: scoping not surfaced")
n = len(sc.get("Entities", []))
if not (sc["MinEntities"] <= n <= sc["MaxEntities"]):
    sys.exit("§14 #2: %d entities outside [%d,%d] (flatten/explode)" % (n, sc["MinEntities"], sc["MaxEntities"]))

# §14 #3 — earned contested: assembled from BOTH directions, inspectable.
fa = md.get("telos.forAgainst", "")
if "evidence_for" not in fa or "evidence_against" not in fa:
    sys.exit("§14 #3: for/against not assembled from both sides: %r" % fa)
if r.get("basis") != "contested":
    sys.exit("§14 #3: basis = %r, want contested" % r.get("basis"))

# §14 #4 — a provenanced contested result is ACCEPTED (banks surplus).
if not r.get("accepted"):
    sys.exit("§14 #4: a provenanced contested result must be accepted")

print("  §14 #1 composite | #2 scope=%d in [%d,%d] | #3 earned-contested | #4 accepted" % (
    n, sc["MinEntities"], sc["MaxEntities"]))
' || fail "§14 structure check failed"

echo ""
echo "PASS: /ping healthy; the planning-root seed CLOSED THE RECURSION on the TREM2"
echo "      question (offline/echo): composite graph → scoped → earned-contested →"
echo "      accepted. Real-model quality is the creds-gated live gate."
