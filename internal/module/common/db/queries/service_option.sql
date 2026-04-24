-- name: ListSortedServiceOption :many
SELECT *
FROM "common"."service_option"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("is_enabled" = ANY(sqlc.slice('is_enabled')) OR sqlc.slice('is_enabled') IS NULL) AND
    ("category" = ANY(sqlc.slice('category')) OR sqlc.slice('category') IS NULL)
)
ORDER BY "priority", "id" ASC
LIMIT sqlc.narg('limit')
OFFSET sqlc.narg('offset');