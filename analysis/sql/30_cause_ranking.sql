-- Rank root causes by total impact (incident-minutes), with count, MTTR
-- and how many alert fires / episodes each cause produced.
-- @learn:sql-cause-ranking
CREATE OR REPLACE TABLE cause_ranking AS
WITH inc AS (
  SELECT cause, incident_id,
         (unix_timestamp(resolved_at) - unix_timestamp(started_at)) / 60.0 AS minutes
  FROM incidents
), noisy AS (
  SELECT cause, COUNT(*) AS episodes, SUM(fires) AS fires
  FROM episode_incident WHERE cause IS NOT NULL GROUP BY cause
)
SELECT inc.cause,
       COUNT(*)                                  AS incidents,
       ROUND(SUM(minutes) / 60.0, 1)             AS impact_hours,
       ROUND(percentile_approx(minutes, 0.5), 1) AS mttr_p50_min,
       ROUND(percentile_approx(minutes, 0.9), 1) AS mttr_p90_min,
       COALESCE(MAX(noisy.episodes), 0)          AS alert_episodes,
       COALESCE(MAX(noisy.fires), 0)             AS alert_fires,
       RANK() OVER (ORDER BY SUM(minutes) DESC)  AS impact_rank,
       RANK() OVER (ORDER BY COUNT(*) DESC)      AS count_rank
FROM inc LEFT JOIN noisy ON noisy.cause = inc.cause
GROUP BY inc.cause
ORDER BY impact_rank;
-- @learn:end sql-cause-ranking
