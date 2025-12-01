-- name: GetShipment :one
SELECT *
FROM "order"."shipment"
WHERE ("id" = sqlc.narg('id'));

-- name: CountShipment :one
SELECT COUNT(*)
FROM "order"."shipment"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("option" = ANY(sqlc.slice('option')) OR sqlc.slice('option') IS NULL) AND
    ("tracking_code" = ANY(sqlc.slice('tracking_code')) OR sqlc.slice('tracking_code') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("label_url" = ANY(sqlc.slice('label_url')) OR sqlc.slice('label_url') IS NULL) AND
    ("cost" = ANY(sqlc.slice('cost')) OR sqlc.slice('cost') IS NULL) AND
    ("cost" > sqlc.narg('cost_from') OR sqlc.narg('cost_from') IS NULL) AND
    ("cost" < sqlc.narg('cost_to') OR sqlc.narg('cost_to') IS NULL) AND
    ("new_cost" = ANY(sqlc.slice('new_cost')) OR sqlc.slice('new_cost') IS NULL) AND
    ("new_cost" > sqlc.narg('new_cost_from') OR sqlc.narg('new_cost_from') IS NULL) AND
    ("new_cost" < sqlc.narg('new_cost_to') OR sqlc.narg('new_cost_to') IS NULL) AND
    ("date_eta" = ANY(sqlc.slice('date_eta')) OR sqlc.slice('date_eta') IS NULL) AND
    ("date_eta" > sqlc.narg('date_eta_from') OR sqlc.narg('date_eta_from') IS NULL) AND
    ("date_eta" < sqlc.narg('date_eta_to') OR sqlc.narg('date_eta_to') IS NULL) AND
    ("from_address" = ANY(sqlc.slice('from_address')) OR sqlc.slice('from_address') IS NULL) AND
    ("to_address" = ANY(sqlc.slice('to_address')) OR sqlc.slice('to_address') IS NULL) AND
    ("weight_grams" = ANY(sqlc.slice('weight_grams')) OR sqlc.slice('weight_grams') IS NULL) AND
    ("weight_grams" > sqlc.narg('weight_grams_from') OR sqlc.narg('weight_grams_from') IS NULL) AND
    ("weight_grams" < sqlc.narg('weight_grams_to') OR sqlc.narg('weight_grams_to') IS NULL) AND
    ("length_cm" = ANY(sqlc.slice('length_cm')) OR sqlc.slice('length_cm') IS NULL) AND
    ("length_cm" > sqlc.narg('length_cm_from') OR sqlc.narg('length_cm_from') IS NULL) AND
    ("length_cm" < sqlc.narg('length_cm_to') OR sqlc.narg('length_cm_to') IS NULL) AND
    ("width_cm" = ANY(sqlc.slice('width_cm')) OR sqlc.slice('width_cm') IS NULL) AND
    ("height_cm" = ANY(sqlc.slice('height_cm')) OR sqlc.slice('height_cm') IS NULL) AND
    ("height_cm" > sqlc.narg('height_cm_from') OR sqlc.narg('height_cm_from') IS NULL) AND
    ("height_cm" < sqlc.narg('height_cm_to') OR sqlc.narg('height_cm_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);

-- name: ListShipment :many
SELECT *
FROM "order"."shipment"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("option" = ANY(sqlc.slice('option')) OR sqlc.slice('option') IS NULL) AND
    ("tracking_code" = ANY(sqlc.slice('tracking_code')) OR sqlc.slice('tracking_code') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("label_url" = ANY(sqlc.slice('label_url')) OR sqlc.slice('label_url') IS NULL) AND
    ("cost" = ANY(sqlc.slice('cost')) OR sqlc.slice('cost') IS NULL) AND
    ("cost" > sqlc.narg('cost_from') OR sqlc.narg('cost_from') IS NULL) AND
    ("cost" < sqlc.narg('cost_to') OR sqlc.narg('cost_to') IS NULL) AND
    ("new_cost" = ANY(sqlc.slice('new_cost')) OR sqlc.slice('new_cost') IS NULL) AND
    ("new_cost" > sqlc.narg('new_cost_from') OR sqlc.narg('new_cost_from') IS NULL) AND
    ("new_cost" < sqlc.narg('new_cost_to') OR sqlc.narg('new_cost_to') IS NULL) AND
    ("date_eta" = ANY(sqlc.slice('date_eta')) OR sqlc.slice('date_eta') IS NULL) AND
    ("date_eta" > sqlc.narg('date_eta_from') OR sqlc.narg('date_eta_from') IS NULL) AND
    ("date_eta" < sqlc.narg('date_eta_to') OR sqlc.narg('date_eta_to') IS NULL) AND
    ("from_address" = ANY(sqlc.slice('from_address')) OR sqlc.slice('from_address') IS NULL) AND
    ("to_address" = ANY(sqlc.slice('to_address')) OR sqlc.slice('to_address') IS NULL) AND
    ("weight_grams" = ANY(sqlc.slice('weight_grams')) OR sqlc.slice('weight_grams') IS NULL) AND
    ("weight_grams" > sqlc.narg('weight_grams_from') OR sqlc.narg('weight_grams_from') IS NULL) AND
    ("weight_grams" < sqlc.narg('weight_grams_to') OR sqlc.narg('weight_grams_to') IS NULL) AND
    ("length_cm" = ANY(sqlc.slice('length_cm')) OR sqlc.slice('length_cm') IS NULL) AND
    ("length_cm" > sqlc.narg('length_cm_from') OR sqlc.narg('length_cm_from') IS NULL) AND
    ("length_cm" < sqlc.narg('length_cm_to') OR sqlc.narg('length_cm_to') IS NULL) AND
    ("width_cm" = ANY(sqlc.slice('width_cm')) OR sqlc.slice('width_cm') IS NULL) AND
    ("height_cm" = ANY(sqlc.slice('height_cm')) OR sqlc.slice('height_cm') IS NULL) AND
    ("height_cm" > sqlc.narg('height_cm_from') OR sqlc.narg('height_cm_from') IS NULL) AND
    ("height_cm" < sqlc.narg('height_cm_to') OR sqlc.narg('height_cm_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountShipment :many
SELECT sqlc.embed(embed_shipment), COUNT(*) OVER() as total_count
FROM "order"."shipment" embed_shipment
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("option" = ANY(sqlc.slice('option')) OR sqlc.slice('option') IS NULL) AND
    ("tracking_code" = ANY(sqlc.slice('tracking_code')) OR sqlc.slice('tracking_code') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("label_url" = ANY(sqlc.slice('label_url')) OR sqlc.slice('label_url') IS NULL) AND
    ("cost" = ANY(sqlc.slice('cost')) OR sqlc.slice('cost') IS NULL) AND
    ("cost" > sqlc.narg('cost_from') OR sqlc.narg('cost_from') IS NULL) AND
    ("cost" < sqlc.narg('cost_to') OR sqlc.narg('cost_to') IS NULL) AND
    ("new_cost" = ANY(sqlc.slice('new_cost')) OR sqlc.slice('new_cost') IS NULL) AND
    ("new_cost" > sqlc.narg('new_cost_from') OR sqlc.narg('new_cost_from') IS NULL) AND
    ("new_cost" < sqlc.narg('new_cost_to') OR sqlc.narg('new_cost_to') IS NULL) AND
    ("date_eta" = ANY(sqlc.slice('date_eta')) OR sqlc.slice('date_eta') IS NULL) AND
    ("date_eta" > sqlc.narg('date_eta_from') OR sqlc.narg('date_eta_from') IS NULL) AND
    ("date_eta" < sqlc.narg('date_eta_to') OR sqlc.narg('date_eta_to') IS NULL) AND
    ("from_address" = ANY(sqlc.slice('from_address')) OR sqlc.slice('from_address') IS NULL) AND
    ("to_address" = ANY(sqlc.slice('to_address')) OR sqlc.slice('to_address') IS NULL) AND
    ("weight_grams" = ANY(sqlc.slice('weight_grams')) OR sqlc.slice('weight_grams') IS NULL) AND
    ("weight_grams" > sqlc.narg('weight_grams_from') OR sqlc.narg('weight_grams_from') IS NULL) AND
    ("weight_grams" < sqlc.narg('weight_grams_to') OR sqlc.narg('weight_grams_to') IS NULL) AND
    ("length_cm" = ANY(sqlc.slice('length_cm')) OR sqlc.slice('length_cm') IS NULL) AND
    ("length_cm" > sqlc.narg('length_cm_from') OR sqlc.narg('length_cm_from') IS NULL) AND
    ("length_cm" < sqlc.narg('length_cm_to') OR sqlc.narg('length_cm_to') IS NULL) AND
    ("width_cm" = ANY(sqlc.slice('width_cm')) OR sqlc.slice('width_cm') IS NULL) AND
    ("height_cm" = ANY(sqlc.slice('height_cm')) OR sqlc.slice('height_cm') IS NULL) AND
    ("height_cm" > sqlc.narg('height_cm_from') OR sqlc.narg('height_cm_from') IS NULL) AND
    ("height_cm" < sqlc.narg('height_cm_to') OR sqlc.narg('height_cm_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateShipment :one
INSERT INTO "order"."shipment" ("id", "option", "tracking_code", "status", "label_url", "cost", "new_cost", "date_eta", "from_address", "to_address", "weight_grams", "length_cm", "width_cm", "height_cm", "date_created")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: CreateBatchShipment :batchone
INSERT INTO "order"."shipment" ("id", "option", "tracking_code", "status", "label_url", "cost", "new_cost", "date_eta", "from_address", "to_address", "weight_grams", "length_cm", "width_cm", "height_cm", "date_created")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: CreateCopyShipment :copyfrom
INSERT INTO "order"."shipment" ("id", "option", "tracking_code", "status", "label_url", "cost", "new_cost", "date_eta", "from_address", "to_address", "weight_grams", "length_cm", "width_cm", "height_cm", "date_created")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: CreateDefaultShipment :one
INSERT INTO "order"."shipment" ("option", "tracking_code", "label_url", "cost", "new_cost", "date_eta", "from_address", "to_address", "weight_grams", "length_cm", "width_cm", "height_cm")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: CreateCopyDefaultShipment :copyfrom
INSERT INTO "order"."shipment" ("option", "tracking_code", "label_url", "cost", "new_cost", "date_eta", "from_address", "to_address", "weight_grams", "length_cm", "width_cm", "height_cm")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- name: UpdateShipment :one
UPDATE "order"."shipment"
SET "option" = COALESCE(sqlc.narg('option'), "option"),
    "tracking_code" = CASE WHEN sqlc.arg('null_tracking_code')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('tracking_code'), "tracking_code") END,
    "status" = COALESCE(sqlc.narg('status'), "status"),
    "label_url" = CASE WHEN sqlc.arg('null_label_url')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('label_url'), "label_url") END,
    "cost" = COALESCE(sqlc.narg('cost'), "cost"),
    "new_cost" = COALESCE(sqlc.narg('new_cost'), "new_cost"),
    "date_eta" = COALESCE(sqlc.narg('date_eta'), "date_eta"),
    "from_address" = COALESCE(sqlc.narg('from_address'), "from_address"),
    "to_address" = COALESCE(sqlc.narg('to_address'), "to_address"),
    "weight_grams" = COALESCE(sqlc.narg('weight_grams'), "weight_grams"),
    "length_cm" = COALESCE(sqlc.narg('length_cm'), "length_cm"),
    "width_cm" = COALESCE(sqlc.narg('width_cm'), "width_cm"),
    "height_cm" = COALESCE(sqlc.narg('height_cm'), "height_cm"),
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteShipment :exec
DELETE FROM "order"."shipment"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("option" = ANY(sqlc.slice('option')) OR sqlc.slice('option') IS NULL) AND
    ("tracking_code" = ANY(sqlc.slice('tracking_code')) OR sqlc.slice('tracking_code') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("label_url" = ANY(sqlc.slice('label_url')) OR sqlc.slice('label_url') IS NULL) AND
    ("cost" = ANY(sqlc.slice('cost')) OR sqlc.slice('cost') IS NULL) AND
    ("cost" > sqlc.narg('cost_from') OR sqlc.narg('cost_from') IS NULL) AND
    ("cost" < sqlc.narg('cost_to') OR sqlc.narg('cost_to') IS NULL) AND
    ("new_cost" = ANY(sqlc.slice('new_cost')) OR sqlc.slice('new_cost') IS NULL) AND
    ("new_cost" > sqlc.narg('new_cost_from') OR sqlc.narg('new_cost_from') IS NULL) AND
    ("new_cost" < sqlc.narg('new_cost_to') OR sqlc.narg('new_cost_to') IS NULL) AND
    ("date_eta" = ANY(sqlc.slice('date_eta')) OR sqlc.slice('date_eta') IS NULL) AND
    ("date_eta" > sqlc.narg('date_eta_from') OR sqlc.narg('date_eta_from') IS NULL) AND
    ("date_eta" < sqlc.narg('date_eta_to') OR sqlc.narg('date_eta_to') IS NULL) AND
    ("from_address" = ANY(sqlc.slice('from_address')) OR sqlc.slice('from_address') IS NULL) AND
    ("to_address" = ANY(sqlc.slice('to_address')) OR sqlc.slice('to_address') IS NULL) AND
    ("weight_grams" = ANY(sqlc.slice('weight_grams')) OR sqlc.slice('weight_grams') IS NULL) AND
    ("weight_grams" > sqlc.narg('weight_grams_from') OR sqlc.narg('weight_grams_from') IS NULL) AND
    ("weight_grams" < sqlc.narg('weight_grams_to') OR sqlc.narg('weight_grams_to') IS NULL) AND
    ("length_cm" = ANY(sqlc.slice('length_cm')) OR sqlc.slice('length_cm') IS NULL) AND
    ("length_cm" > sqlc.narg('length_cm_from') OR sqlc.narg('length_cm_from') IS NULL) AND
    ("length_cm" < sqlc.narg('length_cm_to') OR sqlc.narg('length_cm_to') IS NULL) AND
    ("width_cm" = ANY(sqlc.slice('width_cm')) OR sqlc.slice('width_cm') IS NULL) AND
    ("height_cm" = ANY(sqlc.slice('height_cm')) OR sqlc.slice('height_cm') IS NULL) AND
    ("height_cm" > sqlc.narg('height_cm_from') OR sqlc.narg('height_cm_from') IS NULL) AND
    ("height_cm" < sqlc.narg('height_cm_to') OR sqlc.narg('height_cm_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);