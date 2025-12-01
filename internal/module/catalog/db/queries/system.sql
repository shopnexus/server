-- name: ListStaleSearchSync :many
SELECT id, ref_id, ref_type
FROM catalog.search_sync
WHERE (is_stale_metadata = sqlc.narg('is_stale_metadata') OR is_stale_embedding = sqlc.narg('is_stale_embedding')) AND ref_type = $1
ORDER BY date_updated ASC
FOR UPDATE SKIP LOCKED
LIMIT sqlc.arg('limit');

-- name: UpdateStaleSearchSync :exec
UPDATE catalog.search_sync
SET
    is_stale_metadata = COALESCE(sqlc.narg('is_stale_metadata'), is_stale_metadata),
    is_stale_embedding = COALESCE(sqlc.narg('is_stale_embedding'), is_stale_embedding),
    date_updated = NOW()
WHERE ref_type = sqlc.arg('ref_type') AND ref_id = sqlc.arg('ref_id');

-- name: UpdateBatchStaleSearchSync :batchexec
UPDATE catalog.search_sync
SET
    is_stale_metadata = COALESCE(sqlc.narg('is_stale_metadata'), is_stale_metadata),
    is_stale_embedding = COALESCE(sqlc.narg('is_stale_embedding'), is_stale_embedding),
    date_updated = NOW()
WHERE ref_type = sqlc.arg('ref_type') AND ref_id = sqlc.arg('ref_id');