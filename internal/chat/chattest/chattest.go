// Package chattest opens conversations for the tests of modules that store
// deals, which live in a pair's conversation.
package chattest

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/chat/repository"
)

// Pair returns the ID of the conversation of one and other, opening it
// when the two have none.
func Pair(t *testing.T, db *pgxpool.Pool, one, other uuid.UUID) uuid.UUID {
	t.Helper()
	c, err := repository.NewChats(db).OpenPair(context.Background(), domain.NewConversation(uuid.NewV7(), one, other, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}
