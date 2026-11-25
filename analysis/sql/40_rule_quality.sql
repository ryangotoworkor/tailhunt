-- Per alert rule: how much it fires, how much of that is the same thing
-- re-firing, how often it fires with no incident behind it, and how late
-- it is when it does catch a real incident.
-- @learn:sql-rule-quality
CREATE OR REPLACE TABLE rule_quality AS
WITH lag_per_incident AS (
  SELECT ei.rule, ei.incident_id,
         MIN(unix_timestamp(ei.first_fired) - unix_timestamp(i.started_at)) / 60.0 AS detect_min
  FROM episode_incident ei JOIN incidents i USING (incident_id)
  GROUP BY ei.rule, ei.incident_id
)
SELECT e.rule,
       CAST(SUM(e.fires) AS INTEGER)                                AS fires,
       COUNT(*)                                                     AS episodes,
       ROUND(SUM(e.fires) * 1.0 / COUNT(*), 2)                      AS fires_per_episode,
       SUM(CASE WHEN e.incident_id IS NULL THEN 1 ELSE 0 END)        AS noise_episodes,
       ROUND(AVG(CASE WHEN e.incident_id IS NULL THEN 1.0 ELSE 0.0 END), 3) AS noise_rate,
       COUNT(DISTINCT e.incident_id)                                AS incidents_caught,
       ROUND(MAX(l.p50), 1)                                         AS detect_p50_min,
       ROUND(COUNT(*) / MAX(w.weeks), 1)                            AS episodes_per_week
FROM episode_incident e
LEFT JOIN (SELECT rule, percentile_approx(detect_min, 0.5) AS p50 FROM lag_per_incident GROUP BY rule) l
  ON l.rule = e.rule
CROSS JOIN (SELECT (unix_timestamp(MAX(fired_at)) - unix_timestamp(MIN(fired_at))) / 604800.0 AS weeks FROM alerts) w
GROUP BY e.rule
ORDER BY fires DESC;
-- @learn:end sql-rule-quality
