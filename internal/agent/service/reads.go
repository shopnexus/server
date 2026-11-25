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

// RecordRead is UC-MOD-08 step 3: an MCP tool records what it read of
// people for the member who asked, before it answers (NFR-PRIV-02).
func (s *Agent) RecordRead(ctx context.Context, member uuid.UUID, tool string, kind domain.SubjectKind, ids []uuid.UUID) error {
	if err := s.d.Reads.Insert(ctx, repository.Read{
		ID: uuid.NewV7(), MemberID: member, Tool: tool, SubjectKind: kind, SubjectIDs: ids, At: s.d.Now(),
	}); err != nil {
		return fmt.Errorf("record the agent's read: %w", err)
	}
	return nil
}

// conversationBatch is how many conversations one delete takes; each
// cascades to its turns, calls, events and documents.
const conversationBatch = 100

// SweepReads deletes reads past ReadsKeptFor and blanks the tool outputs
// as old, which hold what the tools read, sweepBatch at a time; then it
// deletes the conversations idle past ConversationsKeptFor.
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
		n, err := s.d.Runs.BlankOutputsBefore(ctx, before, sweepBatch)
		if err != nil {
			return fmt.Errorf("sweep the agent's tool outputs: %w", err)
		}
		if n < sweepBatch {
			break
		}
	}
	idle := now.Add(-ConversationsKeptFor)
	for {
		n, err := s.d.Runs.DeleteIdleConversations(ctx, idle, conversationBatch)
		if err != nil {
			return fmt.Errorf("sweep the agent's idle conversations: %w", err)
		}
		if n < conversationBatch {
			return nil
		}
	}
}

// AccountDeleted blanks the tool outputs of the conversation about a
// deleted account; the worker calls it on account.deleted.
func (s *Agent) AccountDeleted(ctx context.Context, account uuid.UUID) error {
	if err := s.d.Runs.BlankAccountOutputs(ctx, account); err != nil {
		return fmt.Errorf("blank the deleted account's tool outputs: %w", err)
	}
	return nil
}
