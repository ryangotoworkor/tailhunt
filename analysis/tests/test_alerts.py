"""The alert analysis must recover what the generator planted."""

import csv
import filecmp
import os
from collections import Counter
from datetime import datetime
from pathlib import Path

import pytest

import analyze_alerts
import gen_alerts

DATA = Path(__file__).resolve().parents[1] / "data"


@pytest.fixture(scope="module")
def summary():
    return analyze_alerts.analyze(DATA)


def test_committed_data_is_exactly_what_the_seed_generates(tmp_path):
    inc, al = gen_alerts.generate()
    gen_alerts.write(inc, al, tmp_path)
    for f in ["incidents.csv", "alerts.csv", "alerts_truth.csv"]:
        assert filecmp.cmp(tmp_path / f, DATA / f, shallow=False), f"{f} differs from a fresh generation"


# @learn:test-top-cause
def test_planted_top_cause_ranks_first_by_impact(summary):
    # Ground truth straight from incidents.csv, no SQL involved.
    minutes = Counter()
    for r in csv.DictReader(open(DATA / "incidents.csv")):
        t0 = datetime.fromisoformat(r["started_at"])
        t1 = datetime.fromisoformat(r["resolved_at"])
        minutes[r["cause"]] += (t1 - t0).total_seconds() / 60
    truth = minutes.most_common(1)[0][0]
    assert truth == "db_pool_exhaustion", "the generator no longer plants the pool as the top cause"
    assert summary["causes"][0]["cause"] == truth, f"analysis ranks {summary['causes'][0]['cause']} first (rank_by={summary['rank_by']})"
# @learn:end test-top-cause


def test_the_naive_rankings_point_somewhere_else(summary):
    # The reason the analysis exists: volume and count mislead.
    assert summary["naive"]["top_rule_by_fires"] == "DiskUsageHigh"
    assert summary["naive"]["its_noise_rate"] > 0.9
    assert summary["naive"]["top_cause_by_count"] != "db_pool_exhaustion"


def test_time_based_linking_recovers_the_true_noise_rate(summary):
    truth = list(csv.DictReader(open(DATA / "alerts_truth.csv")))
    true_noise = sum(1 for r in truth if not r["incident_id"]) / len(truth)
    est = summary["totals"]["noise_rate_fires"]
    assert abs(est - true_noise) < 0.03, f"estimated {est:.3f}, truth {true_noise:.3f}"


def test_linked_episodes_point_at_the_right_cause(summary):
    # Of all alert fires the SQL attributed to a cause, how many carry that
    # cause in the hidden ground truth? Overlapping incidents make a few wrong.
    con = analyze_alerts.connect(DATA)
    con.execute(f"CREATE TABLE truth AS SELECT * FROM read_csv('{DATA / 'alerts_truth.csv'}', header=true, all_varchar=true)")
    right, total = con.execute(
        """
        SELECT SUM(CASE WHEN t.cause = ei.cause THEN 1 ELSE 0 END), COUNT(*)
        FROM alerts_numbered o JOIN episode_incident ei USING (fingerprint, episode_no)
        JOIN truth t USING (alert_id)
        WHERE ei.cause IS NOT NULL
        """
    ).fetchone()
    assert right / total > 0.9, f"only {right}/{total} attributed fires match the ground truth cause"


def test_dedup_merges_refires(summary):
    t = summary["totals"]
    assert t["episodes"] < t["fires"]
    assert 1.3 < t["dedup_ratio"] < 3, t["dedup_ratio"]
    pool = next(r for r in summary["rules"] if r["rule"] == "InventoryDBPoolSaturated")
    assert pool["noise_rate"] == 0 and "repeat_interval" in pool["action"]


def test_sabotage_switch_changes_the_ranking(monkeypatch):
    monkeypatch.setenv("TAILHUNT_ANALYSIS_RANK", "count")
    s = analyze_alerts.analyze(DATA)
    assert s["causes"][0]["cause"] != "db_pool_exhaustion"
