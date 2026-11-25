#!/usr/bin/env bash
# Load test on the full compose stack, before and after the fix, three
# rounds alternating so a noisy neighbour on the shared colima VM hits
# both sides. Each run: holds at 25/s start 3 s early, then searches at
# 30/s are measured for 20 s. Conditions:
#   baseline  quote healthy, legacy code
#   before    quote +300 ms, legacy code      (the incident)
#   after     quote +300 ms, every fix layer on
# Writes docs/results/loadtest-runs.jsonl (every run) and
# docs/results/loadtest.json + loadtest.md (medians across rounds).
set -euo pipefail
cd "$(dirname "$0")/.."
GW=${GW:-http://localhost:8880}
ROUNDS=${ROUNDS:-3}
DUR=${DUR:-20s}
PY=.venv/bin/python
[ -x $PY ] || { echo "make the venv first: ./scripts/analyze.sh creates it"; exit 1; }
go build -o bin/loadgen ./cmd/loadgen
TOK=$(curl -sf "$GW/api/login" -d '{"username":"oncall","password":"oncall-demo"}' | $PY -c 'import json,sys; print(json.load(sys.stdin)["token"])')
api() { curl -sf -H "Authorization: Bearer $TOK" "$GW/api/admin/$1" ${2:+-d "$2"} >/dev/null; }
drain() { # wait until no connection is in use, so runs do not overlap
  for _ in $(seq 1 120); do
    n=$(curl -sf -H "Authorization: Bearer $TOK" "$GW/api/admin/dbstats" | $PY -c 'import json,sys; print(json.load(sys.stdin)["in_use"])')
    [ "$n" = 0 ] && { sleep 2; return; }
    sleep 1
  done
}
api traffic '{"search_rps":0,"hold_rps":0}'
runs=docs/results/loadtest-runs.jsonl
: > "$runs"
for r in $(seq 1 "$ROUNDS"); do
  for cond in baseline before after; do
    case $cond in
      baseline) api fault '{"latency_ms":0,"ratio":0}';  api mode '{"mode":"legacy"}' ;;
      before)   api fault '{"latency_ms":300,"ratio":1}'; api mode '{"mode":"legacy"}' ;;
      after)    api fault '{"latency_ms":300,"ratio":1}'; api mode '{"mode":"fixed"}' ;;
    esac
    drain
    echo "round $r $cond…" >&2
    ./bin/loadgen -url "$GW" -search-rps 30 -hold-rps 25 -duration "$DUR" -label "$cond" \
      | $PY -c "import json,sys; d=json.load(sys.stdin); d['round']=$r; d['condition']='$cond'; print(json.dumps(d))" >> "$runs"
  done
done
api fault '{"latency_ms":0,"ratio":0}'
api mode '{"mode":"fixed"}'
$PY scripts/bench_summary.py "$runs" docs/results/loadtest.json docs/results/loadtest.md
cat docs/results/loadtest.md
