-- Link each episode to the incident it belongs to. There is no foreign
-- key in the alert stream (on-call rarely links every alert by hand), so
-- the link is by time: the episode starts between 10 minutes before the
-- incident started and the moment it was resolved. When incidents
-- overlap, prefer the one on the same service, then the most recent start.
-- Episodes with no incident are noise.
-- @learn:sql-link
CREATE OR REPLACE TABLE episode_incident AS
WITH candidates AS (
  SELECT e.fingerprint, e.episode_no, i.incident_id, i.cause,
         ROW_NUMBER() OVER (
           PARTITION BY e.fingerprint, e.episode_no
           ORDER BY CASE WHEN e.service = i.service THEN 0 ELSE 1 END, i.started_at DESC) AS pick
  FROM alert_episodes e
  JOIN incidents i
    ON e.first_fired >= i.started_at - INTERVAL 10 MINUTE
   AND e.first_fired <= i.resolved_at
)
SELECT e.*, c.incident_id, c.cause
FROM alert_episodes e
LEFT JOIN candidates c
  ON c.fingerprint = e.fingerprint AND c.episode_no = e.episode_no AND c.pick = 1;
-- @learn:end sql-link
