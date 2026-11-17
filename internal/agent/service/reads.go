package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/agent/domain"
	"github.com/shopnexus/server2/internal/agent/repository"
)

// sweepBatch is how many reads one delete removes, so a sweep after a long
// stop does not hold the table.
const sweepBatch = 10_000

// RecordRead is UC-MOD-08 step 3: an MCP tool records what it read of
// people for the member who asked, before it answers (NFR-PRIV-02).
func (s *Agent) RecordRead(ctx context.Context, member uuid.UUID, tool string, kind domain.SubjectKind, ids []uuid.UUID) error {
	if err := s.reads.Insert(ctx, repository.Read{
		ID: uuid.NewV7(), MemberID: member, Tool: tool, SubjectKind: kind, SubjectIDs: ids, At: s.now(),
	}); err != nil {
		return fmt.Errorf("record the agent's read: %w", err)
	}
	return nil
}

// SweepReads deletes reads past ReadsKeptFor, sweepBatch at a time.
func (s *Agent) SweepReads(ctx context.Context) error {
	before := s.now().Add(-ReadsKeptFor)
	for {
		n, err := s.reads.DeleteBefore(ctx, before, sweepBatch)
		if err != nil {
			return fmt.Errorf("sweep the agent's reads: %w", err)
		}
		if n < sweepBatch {
			return nil
		}
	}
}
