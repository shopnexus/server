-- name: ListDefaultContact :many
SELECT c.* FROM "account"."contact" c
JOIN "account"."profile" p ON c."id" = p."default_contact_id" AND p."id" = ANY(sqlc.slice('account_id'))
ORDER BY c."date_created" DESC;