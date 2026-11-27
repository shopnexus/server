package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
)

// sweepBatch is how many rows one delete or blank changes, so a sweep
// after a long stop does not hold the table.
const sweepBatch = 10_000

// RecordRead records what a record page read of people for the member
// who opened it (NFR-PRIV-02).
func (s *Agent) RecordRead(ctx context.Context, member uuid.UUID, tool string, kind domain.SubjectKind, ids []uuid.UUID) error {
	if err := s.d.Reads.Insert(ctx, repository.Read{
		ID: uuid.NewV7(), MemberID: member, Tool: tool, SubjectKind: kind, SubjectIDs: ids, At: s.d.Now(),
	}); err != nil {
		return fmt.Errorf("record the agent's read: %w", err)
	}
	return nil
}

// conversationBatch is how many conversations one delete takes; each
// cascades to its messages, turns, events, approvals and calls.
const conversationBatch = 100

// SweepReads deletes the reads and the agent's API calls past
// ReadsKeptFor and blanks the command outputs as old, which hold what the
// agent read, sweepBatch at a time; then it deletes the conversations idle
// past ConversationsKeptFor.
func (s *Agent) SweepReads(ctx context.Context) error {
	now := s.d.Now()
	before := now.Add(-ReadsKeptFor)
	for {
		n, err := s.d.Reads.DeleteBefore(ctx, before, sweepBatch)
		if err != nil {
			return fmt.Errorf("sweep the agent's reads: %w", err)
		}
		if n < sweepBatch {
			break
		}
	}
	for {
		n, err := s.d.Conversations.DeleteCallsBefore(ctx, before, sweepBatch)
		if err != nil {
			return fmt.Errorf("sweep the agent's API calls: %w", err)
		}
		if n < sweepBatch {
			break
		}
	}
	for {
		n, err := s.d.Conversations.BlankOutputsBefore(ctx, before, sweepBatch)
		if err != nil {
			return fmt.Errorf("sweep the agent's command outputs: %w", err)
		}
		if n < sweepBatch {
			break
		}
	}
	idle := now.Add(-ConversationsKeptFor)
	for {
		n, err := s.d.Conversations.DeleteIdleConversations(ctx, idle, conversationBatch)
		if err != nil {
			return fmt.Errorf("sweep the agent's idle conversations: %w", err)
		}
		if n < conversationBatch {
			return nil
		}
	}
}

// AccountDeleted blanks the command outputs of the conversation about a
// deleted account; the worker calls it on account.deleted.
func (s *Agent) AccountDeleted(ctx context.Context, account uuid.UUID) error {
	if err := s.d.Conversations.BlankAccountOutputs(ctx, account); err != nil {
		return fmt.Errorf("blank the deleted account's command outputs: %w", err)
	}
	return nil
}
