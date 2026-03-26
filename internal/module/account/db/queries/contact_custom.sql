-- Custom contact queries

-- name: ListDefaultContact :many
SELECT c.*
FROM "account"."contact" c
JOIN "account"."profile" p ON p.default_contact_id = c.id
WHERE p.id = ANY(@account_id::uuid[]);
