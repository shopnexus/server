-- name: ListSortedOption :many
SELECT *
FROM "common"."option"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("is_enabled" = ANY(sqlc.slice('is_enabled')) OR sqlc.slice('is_enabled') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL)
)
ORDER BY "priority", "id" ASC
LIMIT sqlc.narg('limit')
OFFSET sqlc.narg('offset');

-- name: UpsertOption :exec
INSERT INTO "common"."option" (
    "id", "owner_id", "is_enabled", "name", "description", "priority", "logo_rs_id", "data", "type", "provider"
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT ("id") DO UPDATE SET
    "name"        = EXCLUDED."name",
    "description" = EXCLUDED."description",
    "priority"    = EXCLUDED."priority",
    "logo_rs_id"  = EXCLUDED."logo_rs_id",
    "data"        = EXCLUDED."data",
    "type"        = EXCLUDED."type",
    "provider"    = EXCLUDED."provider";