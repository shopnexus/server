-- name: UpdateProfileSettings :one
UPDATE "account"."profile"
SET "settings" = $2,
    "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = $1
RETURNING *;

-- name: UpdateProfileCountry :execrows
UPDATE "account"."profile"
SET "country" = @country,
    "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = @id;

-- name: CreateSignupProfile :one
INSERT INTO "account"."profile" ("id", "country", "settings")
VALUES (@id, @country, @settings)
RETURNING *;
