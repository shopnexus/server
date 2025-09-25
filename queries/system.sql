-- name: ListStaleSyncSearch :many
SELECT ref_id, ref_type
FROM system.search_sync
WHERE is_stale_metadata = true
ORDER BY date_created ASC
LIMIT $1;