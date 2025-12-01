-- name: ListSortedServiceOption :many
SELECT *
FROM "common"."service_option"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("is_active" = ANY(sqlc.slice('is_active')) OR sqlc.slice('is_active') IS NULL) AND
    ("category" = ANY(sqlc.slice('category')) OR sqlc.slice('category') IS NULL)
)
ORDER BY "order", "id" ASC
LIMIT sqlc.narg('limit')
OFFSET sqlc.narg('offset');