-- After a run: the statements that took the database's time, most first,
-- and the ones slowest on average.
\pset footer off
\echo == by total time
SELECT calls, round(total_exec_time::numeric / 1000, 1) AS total_s, round(mean_exec_time::numeric, 2) AS mean_ms,
       round(max_exec_time::numeric, 1) AS max_ms, rows / greatest(calls, 1) AS rows_per_call,
       shared_blks_read AS disk_reads, left(regexp_replace(query, '\s+', ' ', 'g'), 160) AS query
FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
ORDER BY total_exec_time DESC LIMIT 25;
\echo == by mean time, 20 calls or more
SELECT calls, round(mean_exec_time::numeric, 2) AS mean_ms, round(max_exec_time::numeric, 1) AS max_ms,
       left(regexp_replace(query, '\s+', ' ', 'g'), 160) AS query
FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) AND calls >= 20
ORDER BY mean_exec_time DESC LIMIT 15;
