-- name: ListStaleSyncSearch :many
SELECT id, ref_id, ref_type
FROM system.search_sync
WHERE (is_stale_metadata = sqlc.narg('is_stale_metadata') OR is_stale_embedding = sqlc.narg('is_stale_embedding')) AND ref_type = $1
ORDER BY date_created ASC
FOR UPDATE SKIP LOCKED
LIMIT sqlc.arg('limit');
