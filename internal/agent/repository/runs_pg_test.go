package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// A late line from a segment that no longer runs the turn writes nothing,
// and the running segment's lines still land.
func TestStaleSegmentWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	member := uuid.NewV7()
	if _, err := db.Exec(ctx, `
INSERT INTO console_members (id, email, display_name, console_role, status, password_hash, must_change_password,
                           credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@id, @email, 'Lan', 'moderator', 'active', 'x', false, 1, 0, @at, @at, @id)`,
		pgx.StrictNamedArgs{"id": member, "email": member.String() + "@console.test", "at": now}); err != nil {
		t.Fatal(err)
	}
	s := repository.NewRuns(db, slog.New(slog.DiscardHandler))
	c, err := s.OpenConversation(ctx, repository.Conversation{ID: uuid.NewV7(), MemberID: member, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	turn := repository.Turn{
		ID: uuid.NewV7(), ConversationID: c.ID, MemberID: member, Question: "?", AgentID: "a", AgentName: "a",
		ModelAlias: "m", StartedAt: now, RunStartedAt: now, DeadlineMS: 60000,
	}
	if err = s.OpenTurn(ctx, turn, member); err != nil {
		t.Fatal(err)
	}
	stale, live := now.Add(-time.Minute), now
	if ok, werr := s.AppendRunEvent(ctx, turn.ID, stale, "text.delta", nil, true, nil, now); werr != nil || ok {
		t.Fatalf("a stale event: %v %v", ok, werr)
	}
	if err = s.Checkpoint(ctx, turn.ID, stale, json.RawMessage(`{"stale":true}`), true); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveToolCall(ctx, repository.ToolWrite{
		ID: uuid.NewV7(), TurnID: turn.ID, Segment: stale, ToolUseID: "u1", Tool: "ticket_get", Status: "running", At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if ok, werr := s.WriteDocument(ctx, repository.DocumentWrite{
		ID: uuid.NewV7(), ConversationID: c.ID, TurnID: turn.ID, Segment: stale, MemberID: member, Version: 1,
		Title: "d", Sections: json.RawMessage(`[]`), At: now,
	}); werr != nil || ok {
		t.Fatalf("a stale document: %v %v", ok, werr)
	}
	if ok, werr := s.FinishRun(ctx, repository.RunEnd{TurnID: turn.ID, Segment: stale, Status: "succeeded", Answer: "x", At: now}); werr != nil || ok {
		t.Fatalf("a stale end: %v %v", ok, werr)
	}
	got, err := s.Turn(ctx, turn.ID)
	if err != nil || got.Status != "running" || got.EventSeq != 0 || got.Wrote || got.State != nil {
		t.Fatalf("the turn after stale writes: %+v %v", got, err)
	}
	var calls int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM agent_tool_calls WHERE turn_id = @id`,
		pgx.StrictNamedArgs{"id": turn.ID}).Scan(&calls); err != nil || calls != 0 {
		t.Fatalf("stale tool calls: %d %v", calls, err)
	}
	if ok, werr := s.AppendRunEvent(ctx, turn.ID, live, "text.delta", nil, true, nil, now); werr != nil || !ok {
		t.Fatalf("a live event: %v %v", ok, werr)
	}
	if ok, werr := s.FinishRun(ctx, repository.RunEnd{TurnID: turn.ID, Segment: live, Status: "succeeded", Answer: "x", At: now}); werr != nil || !ok {
		t.Fatalf("a live end: %v %v", ok, werr)
	}
}

// A skill's change checks the write it read and finds a deleted skill
// gone; a request key claims one skill and one input.
func TestSkillWritesAndRequestKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	member := uuid.NewV7()
	if _, err := db.Exec(ctx, `
INSERT INTO console_members (id, email, display_name, console_role, status, password_hash, must_change_password,
                           credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@id, @email, 'Lan', 'moderator', 'active', 'x', false, 1, 0, @at, @at, @id)`,
		pgx.StrictNamedArgs{"id": member, "email": member.String() + "@console.test", "at": now}); err != nil {
		t.Fatal(err)
	}
	s := repository.NewRuns(db, slog.New(slog.DiscardHandler))
	key := uuid.NewV7()
	sk := repository.Skill{
		ID: uuid.NewV7(), CreatedBy: member, Name: "a", Slug: "a", Prompt: "?", Variables: json.RawMessage(`[]`),
		CreatedAt: now, UpdatedAt: now, RequestKey: &key, WriteID: uuid.NewV7(),
	}
	if err := s.CreateSkill(ctx, sk); err != nil {
		t.Fatal(err)
	}
	dup := sk
	dup.ID, dup.Slug = uuid.NewV7(), "b"
	if err := s.CreateSkill(ctx, dup); !errors.Is(err, repository.ErrKeyUsed) {
		t.Errorf("the key again: %v", err)
	}
	read := sk.WriteID
	sk.Name, sk.WriteID = "b", uuid.NewV7()
	if err := s.UpdateSkill(ctx, sk, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Errorf("a stale change: %v", err)
	}
	if err := s.UpdateSkill(ctx, sk, read); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSkill(ctx, sk, read); err != nil {
		t.Errorf("the same change again: %v", err)
	}
	if err := s.DeleteSkill(ctx, sk.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSkill(ctx, sk, sk.WriteID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("a change of a deleted skill: %v", err)
	}

	c, err := s.OpenConversation(ctx, repository.Conversation{ID: uuid.NewV7(), MemberID: member, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	turn := repository.Turn{
		ID: uuid.NewV7(), ConversationID: c.ID, MemberID: member, Question: "?", AgentID: "a", AgentName: "a",
		ModelAlias: "m", StartedAt: now, RunStartedAt: now, DeadlineMS: 60000,
	}
	if err = s.OpenTurn(ctx, turn, member); err != nil {
		t.Fatal(err)
	}
	in := repository.Input{ID: uuid.NewV7(), TurnID: turn.ID, Kind: "steer", Text: "x", CreatedAt: now, MemberID: member, RequestKey: &key}
	if err = s.AddInput(ctx, in); err != nil {
		t.Fatal(err)
	}
	again := in
	again.ID = uuid.NewV7()
	if err = s.AddInput(ctx, again); !errors.Is(err, repository.ErrKeyUsed) {
		t.Errorf("the steer's key again: %v", err)
	}
	if got, keyErr := s.InputByKey(ctx, member, key); keyErr != nil || got.ID != in.ID {
		t.Errorf("the key's input %+v %v", got, keyErr)
	}
	if err = s.AddInput(ctx, repository.Input{ID: uuid.NewV7(), TurnID: uuid.NewV7(), Kind: "pause", CreatedAt: now, MemberID: member}); !errors.Is(err, repository.ErrState) {
		t.Errorf("an input to no turn: %v", err)
	}
}
