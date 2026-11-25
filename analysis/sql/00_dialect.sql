-- The analysis SQL is written in the Spark SQL / HiveQL subset and runs on
-- DuckDB. These macros give DuckDB the Spark names used below, so the same
-- queries port to spark.sql(...) with only the table loading changed.
--   Spark / Hive                 DuckDB here
--   percentile_approx(x, q)  ->  quantile_cont(x, q)
--   unix_timestamp(ts)       ->  epoch(ts)
CREATE OR REPLACE MACRO percentile_approx(x, q) AS quantile_cont(x, q);
CREATE OR REPLACE MACRO unix_timestamp(ts) AS epoch(ts);
