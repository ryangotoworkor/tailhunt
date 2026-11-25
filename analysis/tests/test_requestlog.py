"""Request-log SQL on a hand-built log where the answer is known."""

import json
from datetime import datetime, timedelta, timezone

import requestlog


def write_logs(tmp_path):
    t0 = datetime(2026, 12, 1, 12, 0, 0, tzinfo=timezone.utc)
    gw, inv = [], []
    for i in range(300):
        ts = t0 + timedelta(milliseconds=100 * i)  # 30 s, three 10-second windows
        spike = 100 <= i < 200  # the middle window: every search waits ~800 ms for a connection
        wait = 800.0 if spike else 0.1
        tid = f"{i:032x}"
        gw.append(dict(ts=ts.isoformat(), svc="gateway", route="GET /api/search", status=200, dur_ms=wait + 3, db_wait_ms=0, trace_id=tid))
        inv.append(dict(ts=ts.isoformat(), svc="inventory", route="GET /availability", status=200, dur_ms=wait + 1, db_wait_ms=wait, trace_id=tid))
    for name, rows in (("gateway", gw), ("inventory", inv)):
        (tmp_path / f"{name}.jsonl").write_text("".join(json.dumps(r) + "\n" for r in rows))


def test_spike_window_is_found_and_blamed_on_the_pool(tmp_path):
    write_logs(tmp_path)
    s = requestlog.analyze(tmp_path)
    assert s["windows"] == 3 and s["spike_windows"] == 1
    assert s["spike_db_wait_share_median"] > 0.99
    assert s["calm_db_wait_share_median"] < 0.05
    c = s["correlation"]
    assert c["joined_requests"] == 300 and c["slow_requests"] == 100
    assert c["pearson_gateway_vs_db_wait"] > 0.99


def test_endpoint_percentiles(tmp_path):
    write_logs(tmp_path)
    s = requestlog.analyze(tmp_path)
    gw = next(e for e in s["endpoints"] if e["svc"] == "gateway")
    assert gw["requests"] == 300 and gw["p50_ms"] == 3.1 and gw["p99_ms"] == 803.0
