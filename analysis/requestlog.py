"""Request-log analysis: which endpoint is slow, when, and is it the pool?

Reads var/logs/*.jsonl written by the six services (LOG_DIR in compose)
and writes analysis/out/requestlog.json plus analysis/report/requestlog.png.
The numbers in the committed output come from this repo's own load tests
(scripts/bench.sh), not from production.

    .venv/bin/python analysis/requestlog.py [log dir]
"""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path

import duckdb
import pandas as pd

HERE = Path(__file__).parent


def connect(log_dir: Path) -> duckdb.DuckDBPyConnection:
    con = duckdb.connect()
    con.execute((HERE / "sql" / "00_dialect.sql").read_text())
    files = sorted(str(p) for p in log_dir.glob("*.jsonl"))
    if not files:
        raise SystemExit(f"no *.jsonl in {log_dir}; run the stack and some load first")
    con.execute(
        "CREATE TABLE req AS SELECT * FROM read_json(?, format='newline_delimited', "
        "columns={'ts':'TIMESTAMP','svc':'VARCHAR','route':'VARCHAR','status':'INTEGER','dur_ms':'DOUBLE','db_wait_ms':'DOUBLE','trace_id':'VARCHAR'})",
        [files],
    )
    con.execute((HERE / "sql" / "60_requestlog.sql").read_text())
    return con


# @learn:pandas-correlate
def correlate(joined: pd.DataFrame) -> dict:
    """How strongly does end-to-end search latency follow DB pool wait?"""
    j = joined.dropna(subset=["db_wait_ms"])
    slow = j[j["gateway_ms"] > 300]
    return {
        "joined_requests": int(len(j)),
        "pearson_gateway_vs_db_wait": round(float(j["gateway_ms"].corr(j["db_wait_ms"])), 3),
        "slow_requests": int(len(slow)),
        # of the time slow searches spent end to end, how much was waiting for a connection
        "slow_db_wait_share": round(float(slow["db_wait_ms"].sum() / slow["gateway_ms"].sum()), 3) if len(slow) else None,
        "fast_db_wait_share": round(float(j[j["gateway_ms"] <= 300]["db_wait_ms"].sum() / j[j["gateway_ms"] <= 300]["gateway_ms"].sum()), 3),
    }
# @learn:end pandas-correlate


def analyze(log_dir: Path) -> dict:
    con = connect(log_dir)
    endpoints = con.execute("SELECT * FROM endpoint_latency").df()
    windows = con.execute("SELECT * FROM search_windows").df()
    joined = con.execute("SELECT * FROM search_joined").df()
    span = con.execute("SELECT strftime(MIN(ts), '%Y-%m-%d %H:%M:%S'), strftime(MAX(ts), '%Y-%m-%d %H:%M:%S'), COUNT(*) FROM req").fetchone()
    spikes = windows[windows["spike"]]
    return {
        "source": "var/logs/*.jsonl from this repo's own load tests",
        "from": span[0], "until": span[1], "lines": int(span[2]),
        "endpoints": endpoints.to_dict(orient="records"),
        "windows": int(len(windows)),
        "spike_windows": int(len(spikes)),
        "spike_db_wait_share_median": round(float(spikes["db_wait_share"].median()), 3) if len(spikes) else None,
        "calm_db_wait_share_median": round(float(windows[~windows["spike"]]["db_wait_share"].median()), 3),
        "correlation": correlate(joined),
        "_windows": windows,
    }


def chart(windows: pd.DataFrame, out: Path) -> None:
    os.environ.setdefault("MPLCONFIGDIR", str(HERE.parent / ".runtime" / "matplotlib"))
    import matplotlib

    matplotlib.use("Agg")
    import matplotlib.pyplot as plt

    w = windows.reset_index(drop=True)
    fig, ax = plt.subplots(figsize=(8, 3.4))
    x = range(len(w))
    ax.plot(x, w["p99_ms"], color="#1f6f5c", lw=1.6, label="search p99 at the gateway (ms)")
    ax.plot(x, w["db_wait_p99_ms"], color="#b5502f", lw=1.2, ls="--", label="p99 wait for a DB connection (ms)")
    ax.set_yscale("symlog", linthresh=1)  # calm windows wait ~0 ms; plain log would drop them
    ax.axhline(300, color="#c9c2b2", lw=1)
    ax.text(0, 420, "alert threshold 300 ms", fontsize=8, color="#6d675c")
    ax.set_xlabel("10-second windows with traffic (load tests: baseline, before fix, after fix)")
    ax.set_title("Spikes in search p99 line up with DB pool wait", loc="left")
    ax.legend(frameon=False, fontsize=8)
    ax.spines[["top", "right"]].set_visible(False)
    fig.tight_layout()
    out.mkdir(parents=True, exist_ok=True)
    fig.savefig(out / "requestlog.png", dpi=130)
    plt.close(fig)


def main() -> None:
    log_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else HERE.parent / "var" / "logs"
    s = analyze(log_dir)
    chart(s.pop("_windows"), HERE / "report")
    (HERE / "out").mkdir(exist_ok=True)
    (HERE / "out" / "requestlog.json").write_text(json.dumps(s, indent=2, default=str) + "\n")
    c = s["correlation"]
    print(f"{s['lines']} lines, {s['spike_windows']}/{s['windows']} spike windows; corr {c['pearson_gateway_vs_db_wait']}; slow searches spent {c['slow_db_wait_share']} of their time waiting for a connection")


if __name__ == "__main__":
    main()
