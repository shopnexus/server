-- name: ListActivePromotion :many
SELECT *
FROM promotion.promotion
WHERE is_enabled = true
  AND date_started <= NOW()
  AND (date_ended IS NULL OR date_ended > NOW())
  AND (
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL)
    AND (
      ("auto_apply" = sqlc.narg('auto_apply') OR sqlc.narg('auto_apply') IS NULL) OR
      "id" = ANY(sqlc.slice('id')) OR
      "code" = ANY(sqlc.slice('code'))
    )
  )
ORDER BY "group" DESC;
