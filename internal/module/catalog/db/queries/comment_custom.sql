-- Custom comment queries

-- name: CountRepliesByCommentIDs :many
SELECT ref_id, COUNT(*) as reply_count
FROM "catalog"."comment"
WHERE ref_type = 'Comment' AND ref_id = ANY(sqlc.slice('ref_ids'))
GROUP BY ref_id;
