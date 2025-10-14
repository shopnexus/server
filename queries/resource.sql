-- name: ListSortedResources :many
SELECT r.*, rr.ref_id
FROM "shared"."resource_reference" AS rr
INNER JOIN "shared"."resource" AS r ON rr.rs_id = r.id
WHERE
    rr.ref_type = sqlc.arg('ref_type') AND
    rr.ref_id = ANY(sqlc.slice('ref_id')) AND
    (rr.is_primary = sqlc.narg('is_primary') OR sqlc.narg('is_primary') IS NULL)
ORDER BY rr.ref_id, rr."order" ASC;

