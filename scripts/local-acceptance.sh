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

echo "==> POST /invocations"
INV="$(curl -fsS -X POST "${BASE}/invocations" \
  -H 'content-type: application/json' \
  -d '{"prompt":"does X modulate Y, and what is the evidence?"}')"

echo "$INV" | grep -q '"root_id":"root"'        || fail "graph root not instantiated: $INV"
echo "$INV" | grep -q '"node_count":10'         || fail "seed graph not fully instantiated: $INV"
echo "$INV" | grep -q '"standard":"concordant"' || fail "seeded default standard missing: $INV"
for pat in sequential parallel supervisor react; do
  echo "$INV" | grep -q "\"pattern\":\"$pat\"" || fail "pattern $pat not instantiated"
done
# Acceptance present and in a separate envelope (invariant 10).
echo "$INV" | grep -q '"kind":"acceptance"'                || fail "acceptance node absent"
echo "$INV" | python3 -c '
import sys, json
g = json.load(sys.stdin)["graph"]
for n in g["nodes"]:
    if n["kind"] == "acceptance" and n["trust"] == "same-envelope":
        sys.exit("acceptance node shares producer envelope (invariant 10)")
' || fail "acceptance envelope check failed"

# M2: the acceptance node renders a LIVE labeled verdict (not the inert marker).
# With stub producers attaching no provenance, an honest verdict is "not accepted"
# on unprovenanced grounds — which is exactly correct (§4). We assert a real basis
# is present (not the M0 "not-adjudicated-in-M0" inert marker).
echo "$INV" | python3 -c '
import sys, json
md = json.load(sys.stdin).get("metadata", {})
basis = md.get("telos.basis")
if basis is None:
    sys.exit("no verdict basis surfaced — acceptance not rendering")
if basis == "not-adjudicated-in-m0" or "telos.accepted" not in md:
    sys.exit("acceptance not rendering a live verdict (telos.accepted missing)")
' || fail "live acceptance verdict not surfaced"

echo ""
echo "PASS: /ping healthy; /invocations instantiated the 10-node seed graph"
echo "      (sequential+react+supervisor+parallel + separate-envelope acceptance),"
echo "      and the acceptance node rendered a live labeled verdict."
