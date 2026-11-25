#!/usr/bin/env bash
# Allocation profile of the live search service under steady load, legacy
# ranking vs fixed ranking, three rounds alternating. For each run: 60
# searches/s through the gateway, a 10 s delta allocation profile from
# search's /debug/pprof/allocs, and the request count over the same 10 s
# from search's own Prometheus counter, giving bytes allocated per request.
# Writes docs/results/profile-runs.jsonl, profile.md and the pprof -top of
# the last round of each implementation.
set -euo pipefail
cd "$(dirname "$0")/.."
GW=${GW:-http://localhost:8880}
SEARCH=${SEARCH:-http://localhost:8881}
PY=.venv/bin/python
TOK=$(curl -sf "$GW/api/login" -d '{"username":"oncall","password":"oncall-demo"}' | $PY -c 'import json,sys; print(json.load(sys.stdin)["token"])')
api() { curl -sf -H "Authorization: Bearer $TOK" "$GW/api/admin/$1" ${2:+-d "$2"} >/dev/null; }
# $NF, not $2: the route label ("GET /search") contains a space.
count() { curl -sf "$SEARCH/metrics" | awk '/^tailhunt_http_server_duration_seconds_count\{.*route="GET \/search".*svc="search"/ {s+=$NF} END {printf "%d", s}'; }
mkdir -p .runtime/pprof
runs=docs/results/profile-runs.jsonl
: > "$runs"
api fault '{"latency_ms":0,"ratio":0}'
api mode '{"mode":"fixed"}'
api traffic '{"search_rps":60,"hold_rps":0}'
sleep 3
for r in 1 2 3; do
  for impl in legacy fixed; do
    curl -sf "$SEARCH/admin/rank" -d "{\"impl\":\"$impl\"}" >/dev/null
    sleep 2
    c0=$(count)
    curl -sf "$SEARCH/debug/pprof/allocs?seconds=10" -o ".runtime/pprof/$impl-r$r.pb.gz"
    c1=$(count)
    go tool pprof -top -sample_index=alloc_space ".runtime/pprof/$impl-r$r.pb.gz" > ".runtime/pprof/$impl-r$r.top.txt" 2>/dev/null
    $PY - "$impl" "$r" "$((c1 - c0))" ".runtime/pprof/$impl-r$r.top.txt" >> "$runs" <<'PYEOF'
import json, re, sys
impl, rnd, reqs, top = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), open(sys.argv[4]).read()
units = {"B": 1, "kB": 1e3, "MB": 1e6, "GB": 1e9}
m = re.search(r"of ([\d.]+)([kMG]?B) total", top)
total = float(m.group(1)) * units[m.group(2)]
rank = 0.0  # cumulative bytes under the ranking function (its own allocs + fmt.Sprintf etc.)
for line in top.splitlines():
    if re.search(r"search\.rank(Legacy|Fixed)$", line.strip()):
        cum = re.match(r"\s*\S+\s+\S+\s+\S+\s+([\d.]+)([kMG]?B)", line)
        if cum:
            rank = max(rank, float(cum.group(1)) * units[cum.group(2)])
print(json.dumps({"impl": impl, "round": rnd, "requests": reqs, "alloc_bytes": total,
                  "bytes_per_request": round(total / max(reqs, 1)), "rank_cum_bytes": rank,
                  "rank_share": round(rank / total, 3)}))
PYEOF
  done
done
api traffic '{"search_rps":0,"hold_rps":0}'
curl -sf "$SEARCH/admin/rank" -d '{"impl":"fixed"}' >/dev/null
cp .runtime/pprof/legacy-r3.top.txt docs/results/pprof-top-allocs-legacy.txt
cp .runtime/pprof/fixed-r3.top.txt docs/results/pprof-top-allocs-fixed.txt
$PY - "$runs" > docs/results/profile.md <<'PYEOF'
import json, statistics, sys
rs = [json.loads(l) for l in open(sys.argv[1])]
print("| impl | requests per 10 s (median) | bytes allocated per request (median) | each round | share in ranking code |")
print("|---|---:|---:|---|---:|")
for impl in ("legacy", "fixed"):
    x = [r for r in rs if r["impl"] == impl]
    each = " / ".join(str(r["bytes_per_request"]) for r in x)
    print(f"| {impl} | {statistics.median(r['requests'] for r in x):.0f} | {statistics.median(r['bytes_per_request'] for r in x):.0f} B | {each} | {statistics.median(r['rank_share'] for r in x)*100:.1f}% |")
PYEOF
cat docs/results/profile.md
