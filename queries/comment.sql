-- name: UpdateCatalogCommentUpvoteDownvote :exec
UPDATE "catalog"."comment"
SET
    upvote = upvote + COALESCE(sqlc.narg('upvote_delta'), 0),
    downvote = downvote + COALESCE(sqlc.narg('downvote_delta'), 0)
WHERE id = sqlc.arg('id');