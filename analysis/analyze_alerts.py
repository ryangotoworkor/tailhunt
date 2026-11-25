"""Six months of (synthetic) alert history -> which failures hurt most,
which alert rules are noise, and what to change.

SQL does the heavy lifting (analysis/sql/*.sql, Spark-SQL style, run on
DuckDB); pandas turns the result tables into recommendations, charts and
analysis/report.md, plus analysis/out/summary.json for the web UI.

    .venv/bin/python analysis/analyze_alerts.py
"""

from __future__ import annotations

import json
import os
from pathlib import Path

import duckdb
import pandas as pd

HERE = Path(__file__).parent
SQL = HERE / "sql"
DATA = HERE / "data"


def connect(data: Path = DATA) -> duckdb.DuckDBPyConnection:
    con = duckdb.connect()
    inc_types = {"started_at": "TIMESTAMP", "resolved_at": "TIMESTAMP"}
    alert_types = {"fired_at": "TIMESTAMP", "resolved_at": "TIMESTAMP"}
    con.execute(f"CREATE TABLE incidents AS SELECT * FROM read_csv('{data / 'incidents.csv'}', header=true, types={inc_types})")
    con.execute(f"CREATE TABLE alerts AS SELECT * FROM read_csv('{data / 'alerts.csv'}', header=true, types={alert_types})")
    for f in ["00_dialect.sql", "10_episodes.sql", "20_link.sql", "30_cause_ranking.sql", "40_rule_quality.sql", "50_monthly.sql"]:
        con.execute((SQL / f).read_text())
    return con


def rank_column() -> str:
    """Which column decides the cause ranking. TAILHUNT_ANALYSIS_RANK=count
    is the sabotage switch: it ranks by incident count, the naive choice,
    and the test that pins the planted top cause must then fail."""
    return "count_rank" if os.environ.get("TAILHUNT_ANALYSIS_RANK") == "count" else "impact_rank"


# @learn:rule-advice
def advise(rules: pd.DataFrame) -> pd.DataFrame:
    """Turn rule statistics into a concrete change per rule."""
    out = []
    for r in rules.itertuples():
        if r.noise_rate >= 0.8 and r.episodes_per_week >= 3:
            action = "demote to ticket or raise the threshold: it pages for nothing most of the time"
        elif r.noise_rate >= 0.9 and r.fires_per_episode <= 1.05:
            action = "route to a ticket: a preventive warning (one fire per episode), not a page"
        elif r.fires_per_episode >= 3:
            action = "add for / repeat_interval: it re-fires several times per episode"
        elif r.noise_rate <= 0.2 and r.incidents_caught >= 10:
            action = "keep paging: precise and catches many incidents"
        else:
            action = "keep"
        out.append({**r._asdict(), "action": action})
    return pd.DataFrame(out).drop(columns=["Index"])
# @learn:end rule-advice


def analyze(data: Path = DATA) -> dict:
    con = connect(data)
    causes = con.execute("SELECT * FROM cause_ranking").df()
    causes = causes.sort_values([rank_column(), "cause"]).reset_index(drop=True)
    rules = advise(con.execute("SELECT * FROM rule_quality").df())
    monthly = con.execute("SELECT * FROM monthly").df()
    totals = con.execute(
        """SELECT (SELECT COUNT(*) FROM alerts) AS fires,
                  (SELECT COUNT(*) FROM alert_episodes) AS episodes,
                  (SELECT COUNT(*) FROM episode_incident WHERE incident_id IS NULL) AS noise_episodes,
                  (SELECT SUM(fires) FROM episode_incident WHERE incident_id IS NULL) AS noise_fires,
                  (SELECT COUNT(*) FROM incidents) AS incidents,
                  (SELECT COUNT(DISTINCT incident_id) FROM episode_incident WHERE incident_id IS NOT NULL) AS incidents_alerted,
                  (SELECT strftime(MIN(fired_at), '%Y-%m-%d') FROM alerts) AS since,
                  (SELECT strftime(MAX(fired_at), '%Y-%m-%d') FROM alerts) AS until"""
    ).df().iloc[0].to_dict()
    totals = {k: (int(v) if hasattr(v, "__int__") and not isinstance(v, str) else v) for k, v in totals.items()}
    totals["noise_rate_fires"] = round(totals["noise_fires"] / totals["fires"], 3)
    totals["noise_rate_episodes"] = round(totals["noise_episodes"] / totals["episodes"], 3)
    totals["dedup_ratio"] = round(totals["fires"] / totals["episodes"], 2)
    naive_top_rule = rules.sort_values("fires", ascending=False).iloc[0]
    return {
        "synthetic": True,
        "generator": "analysis/gen_alerts.py (seed 20261125)",
        "totals": totals,
        "noise_rate_truth_note": "ground truth lives in data/alerts_truth.csv; the SQL never reads it",
        "rank_by": rank_column().replace("_rank", ""),
        "causes": causes.to_dict(orient="records"),
        "rules": rules.to_dict(orient="records"),
        "monthly": monthly.to_dict(orient="records"),
        "naive": {
            "top_rule_by_fires": naive_top_rule["rule"],
            "its_noise_rate": float(naive_top_rule["noise_rate"]),
            "top_cause_by_count": causes.sort_values(["count_rank", "cause"]).iloc[0]["cause"],
        },
    }


def charts(s: dict, out: Path) -> None:
    os.environ.setdefault("MPLCONFIGDIR", str(HERE.parent / ".runtime" / "matplotlib"))
    import matplotlib

    matplotlib.use("Agg")
    import matplotlib.pyplot as plt

    out.mkdir(parents=True, exist_ok=True)
    ink, accent, muted = "#2b2a26", "#1f6f5c", "#c9c2b2"
    plt.rcParams.update({"font.size": 10, "axes.edgecolor": muted, "axes.labelcolor": ink, "xtick.color": ink, "ytick.color": ink})

    c = pd.DataFrame(s["causes"]).sort_values("impact_hours")
    fig, ax = plt.subplots(figsize=(7.5, 3.6))
    colors = [accent if x == c["impact_hours"].max() else muted for x in c["impact_hours"]]
    ax.barh(c["cause"], c["impact_hours"], color=colors)
    for y, (h, n) in enumerate(zip(c["impact_hours"], c["incidents"])):
        ax.text(h, y, f"  {h:.0f} h · {n} incidents", va="center", fontsize=8, color=ink)
    ax.set_xlabel("total incident time, hours (6 months, synthetic)")
    ax.set_title("Root causes ranked by impact", loc="left", color=ink)
    ax.spines[["top", "right"]].set_visible(False)
    ax.set_xlim(0, c["impact_hours"].max() * 1.35)
    fig.tight_layout()
    fig.savefig(out / "causes.png", dpi=130)
    plt.close(fig)

    r = pd.DataFrame(s["rules"]).sort_values("fires")
    fig, ax = plt.subplots(figsize=(7.5, 3.8))
    real = r["fires"] * (1 - r["noise_rate"])
    ax.barh(r["rule"], real, color=accent, label="fires during an incident")
    ax.barh(r["rule"], r["fires"] - real, left=real, color=muted, label="fires with no incident (noise)")
    ax.set_xlabel("alert fires (6 months, synthetic)")
    ax.set_title("Alert rules: volume vs. signal", loc="left", color=ink)
    ax.legend(frameon=False, fontsize=8, loc="lower right")
    ax.spines[["top", "right"]].set_visible(False)
    fig.tight_layout()
    fig.savefig(out / "rules.png", dpi=130)
    plt.close(fig)


def report(s: dict) -> str:
    t = s["totals"]
    causes = pd.DataFrame(s["causes"])
    rules = pd.DataFrame(s["rules"])
    top = causes.iloc[0]
    lines = [
        "# Alert history analysis",
        "",
        "> **The data is synthetic.** It is generated by `analysis/gen_alerts.py` from a fixed seed (20261125):"
        " six months of incidents and alerts whose shape imitates a real on-call history (log-normal resolve times,"
        " flapping re-fires, noisy rules). No real company data is in this repository. Regenerate with"
        " `.venv/bin/python analysis/gen_alerts.py`; the tests check the committed CSVs are exactly what the seed produces.",
        "",
        f"Window {t['since']} .. {t['until']}: **{t['incidents']} incidents**, **{t['fires']} alert fires**, "
        f"which collapse into **{t['episodes']} episodes** (re-fires of the same rule/service within 30 min merged; "
        f"{t['dedup_ratio']} fires per episode). "
        f"**{100 * t['noise_rate_episodes']:.1f}%** of episodes ({100 * t['noise_rate_fires']:.1f}% of fires) had no incident behind them.",
        "",
        "## 1. Which failures hurt most",
        "",
        "![causes](report/causes.png)",
        "",
        "| rank | cause | incidents | impact (h) | MTTR p50 (min) | MTTR p90 (min) | alert episodes | rank by count |",
        "|---:|---|---:|---:|---:|---:|---:|---:|",
    ]
    for r in causes.itertuples():
        lines.append(
            f"| {r.impact_rank} | {r.cause} | {r.incidents} | {r.impact_hours} | {r.mttr_p50_min} | {r.mttr_p90_min} | {r.alert_episodes} | {r.count_rank} |"
        )
    lines += [
        "",
        f"**{top['cause']}** is first by total impact ({top['impact_hours']} h) although it is "
        f"#{top['count_rank']} by count: fewer incidents than `{s['naive']['top_cause_by_count']}`, but each one lasts longer"
        f" (MTTR p50 {top['mttr_p50_min']} min). Counting incidents would have sent the next quarter's work to the wrong place.",
        "",
        "## 2. Which alert rules are noise",
        "",
        "![rules](report/rules.png)",
        "",
        "| rule | fires | episodes | fires/episode | noise rate | incidents caught | detection p50 (min) | episodes/week | change |",
        "|---|---:|---:|---:|---:|---:|---:|---:|---|",
    ]
    for r in rules.itertuples():
        det = "" if pd.isna(r.detect_p50_min) else r.detect_p50_min
        lines.append(
            f"| {r.rule} | {r.fires} | {r.episodes} | {r.fires_per_episode} | {100 * r.noise_rate:.0f}% | {r.incidents_caught} | {det} | {r.episodes_per_week} | {r.action} |"
        )
    lines += [
        "",
        f"The loudest rule, **{s['naive']['top_rule_by_fires']}**, is {100 * s['naive']['its_noise_rate']:.0f}% noise. "
        "Ranking problems by alert volume would point at disks; ranking by impact points at the connection pool.",
        "",
        "## 3. Method",
        "",
        "1. **Episodes** (`sql/10_episodes.sql`): gaps-and-islands over each fingerprint; a fire within 30 min of the previous resolve continues the episode.",
        "2. **Link to incidents** (`sql/20_link.sql`): an episode belongs to an incident if it starts between 10 min before the incident and its resolve; overlaps prefer the same service, then the latest start. No hand-made links are used.",
        "3. **Cause ranking** (`sql/30_cause_ranking.sql`): total incident-minutes, MTTR p50/p90 per cause.",
        "4. **Rule quality** (`sql/40_rule_quality.sql`): noise rate = episodes with no incident / episodes; detection lag = first episode start - incident start.",
        "5. **Advice** (`analyze_alerts.py::advise`): thresholds on noise rate, fires per episode and incidents caught.",
        "",
        "The SQL uses Spark SQL names (`percentile_approx`, `unix_timestamp`) and runs on DuckDB through two macros in"
        " `sql/00_dialect.sql`; DuckDB stands in for Hive/Spark SQL so the analysis runs on a laptop.",
        "",
        "Checked by `analysis/tests/test_alerts.py`: the planted top cause ranks first, the linking recovers the"
        " generator's ground truth (`data/alerts_truth.csv`, never read by the SQL) within tolerance, and the naive"
        " rankings really do point elsewhere.",
        "",
    ]
    return "\n".join(lines)


def main() -> None:
    s = analyze()
    out = HERE / "out"
    out.mkdir(exist_ok=True)
    (out / "summary.json").write_text(json.dumps(s, indent=2, default=str) + "\n")
    charts(s, HERE / "report")
    (HERE / "report.md").write_text(report(s))
    print(f"top cause by {s['rank_by']}: {s['causes'][0]['cause']}; noise rate (episodes) {s['totals']['noise_rate_episodes']}")


if __name__ == "__main__":
    main()
