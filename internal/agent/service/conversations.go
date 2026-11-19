package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/authn"
)

// listLimit is the most conversations a list returns.
const listLimit = 50

// visible says whether caller may open c: every conversation with a
// subject, and the caller's own.
func visible(c repository.Conversation, caller authn.ConsoleMember) bool {
	return c.SubjectKind != "" || c.MemberID == caller.MemberID
}

func conversationView(c repository.Conversation) Conversation {
	return Conversation{
		ID: c.ID, SubjectKind: domain.SubjectKind(c.SubjectKind), SubjectID: c.SubjectID, Title: c.Title, UpdatedAt: c.LastActivityAt,
	}
}

// Conversations is UC-MOD-08 step 1: the member finds the conversation of a
// record, those about records of a kind, their own that mention a record,
// or all their own.
func (s *Agent) Conversations(ctx context.Context, caller authn.ConsoleMember, q ConversationQuery) ([]Conversation, error) {
	if q.SubjectID != nil && q.SubjectKind == "" {
		return nil, domain.ErrInvalidSubject
	}
	mention := q.MentionKind != "" || q.MentionID != nil
	if mention && (q.MentionID == nil || !slices.Contains(domain.AttachmentKinds, q.MentionKind) || q.SubjectKind != "") {
		return nil, domain.ErrRequestInvalid
	}
	list, err := s.runs.Conversations(ctx, caller.MemberID, repository.ConversationFilter{
		SubjectKind: string(q.SubjectKind), SubjectID: q.SubjectID, MentionKind: q.MentionKind, MentionID: q.MentionID,
	}, listLimit)
	if err != nil {
		return nil, fmt.Errorf("list the conversations: %w", err)
	}
	out := make([]Conversation, 0, len(list))
	for i := range list {
		out = append(out, conversationView(list[i]))
	}
	return out, nil
}

// CreateConversation is UC-MOD-08 step 1: the member opens the record's
// conversation, which every member shares and the first member makes, or
// a new one of their own.
func (s *Agent) CreateConversation(ctx context.Context, caller authn.ConsoleMember, in NewConversation) (Conversation, error) {
	if (in.SubjectKind == "") != (in.SubjectID == nil) {
		return Conversation{}, domain.ErrInvalidSubject
	}
	c, err := s.runs.OpenConversation(ctx, repository.Conversation{
		ID: uuid.NewV7(), MemberID: caller.MemberID, SubjectKind: string(in.SubjectKind), SubjectID: in.SubjectID,
		CreatedAt: s.now().UTC(),
	})
	if err != nil {
		return Conversation{}, fmt.Errorf("open the conversation: %w", err)
	}
	return conversationView(c), nil
}

// Conversation is UC-MOD-08 step 1: the member reads a conversation, each
// question with the member who asked it.
func (s *Agent) Conversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConversationDetail, error) {
	c, err := s.runs.Conversation(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && !visible(c, caller)) {
		return ConversationDetail{}, domain.ErrConversationNotFound
	}
	if err != nil {
		return ConversationDetail{}, fmt.Errorf("read the conversation: %w", err)
	}
	turns, err := s.runs.Turns(ctx, id)
	if err != nil {
		return ConversationDetail{}, fmt.Errorf("read the conversation's turns: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(turns))
	for i := range turns {
		ids = append(ids, turns[i].ID)
	}
	steers, err := s.runs.Steers(ctx, ids)
	if err != nil {
		return ConversationDetail{}, fmt.Errorf("read the turns' steers: %w", err)
	}
	out := ConversationDetail{Conversation: conversationView(c), Turns: make([]Turn, 0, len(turns))}
	for i := range turns {
		out.Turns = append(out.Turns, turnView(turns[i], steers[turns[i].ID]))
	}
	return out, nil
}

// changeable is the conversation caller may change: their own, any for an
// admin; a shared one is an admin's (agent_forbidden), another member's
// own is not found, as for reading.
func (s *Agent) changeable(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) error {
	c, err := s.runs.Conversation(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrConversationNotFound
	}
	if err != nil {
		return fmt.Errorf("read the conversation: %w", err)
	}
	admin := caller.Role == "admin"
	switch {
	case c.SubjectKind == "" && c.MemberID != caller.MemberID && !admin:
		return domain.ErrConversationNotFound
	case c.SubjectKind != "" && !admin:
		return domain.ErrForbidden
	}
	return nil
}

// RenameConversation is UC-MOD-08: the member names a conversation.
func (s *Agent) RenameConversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, title string) (Conversation, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 80 {
		return Conversation{}, domain.ErrTitleInvalid
	}
	if err := s.changeable(ctx, caller, id); err != nil {
		return Conversation{}, err
	}
	c, err := s.runs.RenameConversation(ctx, id, title, s.now().UTC())
	if errors.Is(err, repository.ErrNotFound) {
		return Conversation{}, domain.ErrConversationNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("rename the conversation: %w", err)
	}
	return conversationView(c), nil
}

// DeleteConversation is UC-MOD-08: the member deletes a conversation with
// its turns, events and documents.
func (s *Agent) DeleteConversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) error {
	if err := s.changeable(ctx, caller, id); err != nil {
		return err
	}
	switch err := s.runs.DeleteConversation(ctx, id); {
	case errors.Is(err, repository.ErrNotFound):
		return domain.ErrConversationNotFound
	case errors.Is(err, repository.ErrBusy):
		return domain.ErrTurnInProgress
	case err != nil:
		return fmt.Errorf("delete the conversation: %w", err)
	}
	return nil
}

// turnView is a turn as the console reads it, with its steers.
func turnView(t repository.Turn, steers []repository.Input) Turn {
	st := stateOf(t)
	v := Turn{
		ID: t.ID, ConversationID: &t.ConversationID, MemberID: t.MemberID, Status: t.Status, Text: t.Question,
		Attachments: t.Attachments, Blocks: rawList(t.Blocks), Sources: []Source{}, Outcome: domain.Outcome(t.Status, t.ErrorCode), ErrorCode: t.ErrorCode,
		Continuable: domain.Continuable(t.Status, t.ErrorCode, t.Wrote, t.Latest),
		Agent:       &TurnAgent{ID: t.AgentID, Name: t.AgentName},
		Usage: &Usage{
			InputTokens: int(t.Usage.InputTokens), OutputTokens: int(t.Usage.OutputTokens),
			CacheReadTokens: new(int(t.Usage.CacheReadTokens)), CacheWriteTokens: new(int(t.Usage.CacheWriteTokens)),
			CostUSD: t.Usage.CostUSD, Provider: t.Usage.Provider,
		},
		Steers: make([]Steer, 0, len(steers)), LastSeq: int(t.EventSeq), CreatedAt: t.StartedAt, FinishedAt: t.FinishedAt,
		DurationMS: t.LatencyMS,
	}
	if t.Answer != nil {
		v.Answer = *t.Answer
	}
	if t.Usage.FirstTokenMS != nil {
		v.Usage.FirstTokenMS = new(int(*t.Usage.FirstTokenMS))
	}
	_ = json.Unmarshal(sourcesOf(t), &v.Sources)
	if v.Sources == nil {
		v.Sources = []Source{}
	}
	for i := range steers {
		v.Steers = append(v.Steers, Steer{SteerID: steers[i].ID, Text: steers[i].Text})
	}
	if t.Status == domain.TurnAwaitingInput && nullable(st.PendingAsk) != nil {
		_ = json.Unmarshal(st.PendingAsk, &v.Questions)
	}
	return v
}
