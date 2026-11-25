-- Incidents and impact per month and cause, for the trend chart.
CREATE OR REPLACE TABLE monthly AS
SELECT strftime(started_at, '%Y-%m') AS month, cause,
       COUNT(*) AS incidents,
       ROUND(SUM(unix_timestamp(resolved_at) - unix_timestamp(started_at)) / 3600.0, 1) AS impact_hours
FROM incidents
GROUP BY 1, 2
ORDER BY 1, 2;
