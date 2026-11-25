-- Request logs (JSON Lines, one per request per service) -> latency per
-- endpoint, 10-second windows with a p99 spike, and how much of the
-- gateway's time in those windows was spent waiting for a DB connection.
-- Expects a table req(ts, svc, route, status, dur_ms, db_wait_ms, trace_id).

-- @learn:sql-endpoint-latency
CREATE OR REPLACE TABLE endpoint_latency AS
SELECT svc, route, COUNT(*) AS requests,
       ROUND(percentile_approx(dur_ms, 0.50), 1) AS p50_ms,
       ROUND(percentile_approx(dur_ms, 0.95), 1) AS p95_ms,
       ROUND(percentile_approx(dur_ms, 0.99), 1) AS p99_ms,
       ROUND(AVG(CASE WHEN status >= 500 THEN 1.0 ELSE 0.0 END), 4) AS error_rate
FROM req
GROUP BY svc, route
HAVING COUNT(*) >= 50
ORDER BY svc, route;
-- @learn:end sql-endpoint-latency

-- The same search, seen twice: once at the gateway (end-to-end) and once
-- at inventory (with the time it waited for a pooled connection). They
-- share trace_id because the trace context is propagated on every hop.
-- @learn:sql-trace-join
CREATE OR REPLACE TABLE search_joined AS
SELECT g.ts, g.trace_id, g.dur_ms AS gateway_ms, g.status,
       i.dur_ms AS inventory_ms, i.db_wait_ms
FROM req g
JOIN req i ON i.trace_id = g.trace_id AND i.svc = 'inventory' AND i.route = 'GET /availability'
WHERE g.svc = 'gateway' AND g.route = 'GET /api/search' AND g.trace_id <> '';
-- @learn:end sql-trace-join

-- @learn:sql-spike-windows
CREATE OR REPLACE TABLE search_windows AS
SELECT time_bucket(INTERVAL 10 SECOND, ts) AS window_start,
       COUNT(*) AS requests,
       ROUND(percentile_approx(gateway_ms, 0.99), 1) AS p99_ms,
       ROUND(percentile_approx(db_wait_ms, 0.99), 1) AS db_wait_p99_ms,
       ROUND(SUM(db_wait_ms) / SUM(gateway_ms), 3) AS db_wait_share,
       percentile_approx(gateway_ms, 0.99) > 300 AS spike
FROM search_joined
GROUP BY 1
HAVING COUNT(*) >= 20
ORDER BY 1;
-- @learn:end sql-spike-windows
