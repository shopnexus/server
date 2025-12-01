-- name: ListSortedResources :many
SELECT r.*, rr.ref_id
FROM "common"."resource_reference" AS rr
INNER JOIN "common"."resource" AS r ON rr.rs_id = r.id
WHERE
    rr.ref_type = sqlc.arg('ref_type') AND
    rr.ref_id = ANY(sqlc.slice('ref_id'))
ORDER BY rr."order", rr.id ASC;