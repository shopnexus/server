-- name: UpdateProfileSettings :one
UPDATE "account"."profile"
SET "settings" = $2,
    "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = $1
RETURNING *;
