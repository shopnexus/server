// Package repository stores module agent's record of the agent's reads of
// personal data.
package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/agent/domain"
)

// Read is one read of personal data an MCP tool made for a member: the
// tool, what it read and the ids it returned.
type Read struct {
	ID          uuid.UUID
	MemberID    uuid.UUID
	Tool        string
	SubjectKind domain.SubjectKind
	SubjectIDs  []uuid.UUID
	At          time.Time
}

// Reads keeps console_agent_reads. Each method is its own statement.
type Reads interface {
	// Insert stores r; the same id again does nothing.
	Insert(ctx context.Context, r Read) error
	// DeleteBefore deletes at most limit reads made earlier than before,
	// and returns how many it deleted.
	DeleteBefore(ctx context.Context, before time.Time, limit int) (int, error)
}
