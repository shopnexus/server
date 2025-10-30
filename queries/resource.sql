-- name: ListSortedResources :many
SELECT r.*, rr.ref_id
FROM "shared"."resource_reference" AS rr
INNER JOIN "shared"."resource" AS r ON rr.rs_id = r.id
WHERE
    rr.ref_type = sqlc.arg('ref_type') AND
    rr.ref_id = ANY(sqlc.slice('ref_id')) AND
    (rr.is_primary = sqlc.narg('is_primary') OR sqlc.narg('is_primary') IS NULL)
ORDER BY rr.ref_id, rr."order" ASC;

-- name: SearchSharedResource :many
SELECT *
FROM "shared"."resource"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("id" > sqlc.narg('id_from') OR sqlc.narg('id_from') IS NULL) AND
    ("id" < sqlc.narg('id_to') OR sqlc.narg('id_to') IS NULL) AND
    ("uploaded_by" = ANY(sqlc.slice('uploaded_by')) OR sqlc.slice('uploaded_by') IS NULL) AND
    ("uploaded_by" > sqlc.narg('uploaded_by_from') OR sqlc.narg('uploaded_by_from') IS NULL) AND
    ("uploaded_by" < sqlc.narg('uploaded_by_to') OR sqlc.narg('uploaded_by_to') IS NULL) AND
    ("object_key" = ANY(sqlc.slice('object_key')) OR sqlc.slice('object_key') IS NULL) AND
    ("size" = ANY(sqlc.slice('size')) OR sqlc.slice('size') IS NULL) AND
    ("size" > sqlc.narg('size_from') OR sqlc.narg('size_from') IS NULL) AND
    ("size" < sqlc.narg('size_to') OR sqlc.narg('size_to') IS NULL) AND
    ("metadata" = ANY(sqlc.slice('metadata')) OR sqlc.slice('metadata') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("created_at" = ANY(sqlc.slice('created_at')) OR sqlc.slice('created_at') IS NULL) AND
    ("created_at" > sqlc.narg('created_at_from') OR sqlc.narg('created_at_from') IS NULL) AND
    ("created_at" < sqlc.narg('created_at_to') OR sqlc.narg('created_at_to') IS NULL) AND 
    ("provider" = ANY(sqlc.slice('provider')) OR sqlc.slice('provider') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')
OFFSET sqlc.narg('offset');