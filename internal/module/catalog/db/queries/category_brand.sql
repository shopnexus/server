-- name: SearchBrand :many
SELECT 
    sqlc.embed(brand),
    COUNT(*) OVER() as total_count
FROM "catalog"."brand" brand
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    (
        "name" ILIKE '%' || sqlc.narg('search') || '%' OR
        "description" ILIKE '%' || sqlc.narg('search') || '%'  OR
        sqlc.narg('search') IS NULL 
    )
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: SearchCategory :many
SELECT 
    sqlc.embed(category),
    COUNT(*) OVER() as total_count
FROM "catalog"."category" category
WHERE (
   ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    (
        "name" ILIKE '%' || sqlc.narg('search') || '%' OR
        "description" ILIKE '%' || sqlc.narg('search') || '%'  OR
        sqlc.narg('search') IS NULL 
    )
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;
