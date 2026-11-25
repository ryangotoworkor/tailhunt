-- Collapse re-fires into episodes ("gaps and islands"). Alerts with the
-- same fingerprint (rule/service) belong to one episode while each fire
-- starts within 30 minutes of the previous one's resolve. One episode is
-- what a human experiences as "that alert went off".
-- @learn:sql-episodes
CREATE OR REPLACE TABLE alerts_numbered AS
WITH ordered AS (
  SELECT *,
         CASE WHEN unix_timestamp(fired_at)
                   - unix_timestamp(LAG(resolved_at) OVER (PARTITION BY fingerprint ORDER BY fired_at)) <= 30 * 60
              THEN 0 ELSE 1 END AS is_new
  FROM alerts
)
SELECT *, SUM(is_new) OVER (PARTITION BY fingerprint ORDER BY fired_at ROWS UNBOUNDED PRECEDING) AS episode_no
FROM ordered;

CREATE OR REPLACE TABLE alert_episodes AS
SELECT fingerprint, episode_no,
       MIN(rule) AS rule, MIN(service) AS service, MIN(severity) AS severity,
       MIN(fired_at) AS first_fired, MAX(resolved_at) AS last_resolved,
       COUNT(*) AS fires
FROM alerts_numbered
GROUP BY fingerprint, episode_no;
-- @learn:end sql-episodes
