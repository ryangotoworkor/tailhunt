"""Median of each metric per condition across rounds -> loadtest.json / .md."""
import json
import statistics
import sys

runs = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
conds = ["baseline", "before", "after"]
keys = ["p50_ms", "p95_ms", "p99_ms", "max_ms", "error_rate"]
out = {"rounds": max(r["round"] for r in runs), "runs": len(runs), "conditions": {}}
for c in conds:
    rs = [r for r in runs if r["condition"] == c]
    out["conditions"][c] = {
        "requests_per_run": rs[0]["requests"],
        **{k: round(statistics.median(r[k] for r in rs), 2 if k != "error_rate" else 4) for k in keys},
        "p99_ms_each_round": [r["p99_ms"] for r in rs],
        "hold_error_rate_median": round(statistics.median(r["extra"]["holds"]["error_rate"] for r in rs), 4),
    }
json.dump(out, open(sys.argv[2], "w"), indent=2)
open(sys.argv[2], "a").write("\n")
lines = [
    "| condition | searches/run | p50 | p95 | p99 (median) | p99 each round | search errors | hold errors |",
    "|---|---:|---:|---:|---:|---|---:|---:|",
]
for c in conds:
    d = out["conditions"][c]
    each = " / ".join(f"{x:.0f}" for x in d["p99_ms_each_round"])
    lines.append(
        f"| {c} | {d['requests_per_run']} | {d['p50_ms']:.1f} ms | {d['p95_ms']:.1f} ms | **{d['p99_ms']:.1f} ms** | {each} ms | "
        f"{100*d['error_rate']:.2f}% | {100*d['hold_error_rate_median']:.2f}% |"
    )
open(sys.argv[3], "w").write("\n".join(lines) + "\n")
