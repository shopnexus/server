package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/authn"
)

// conversationPage is the most conversations a list returns.
const conversationPage = 100

// visible says whether caller may open c: one about a record is every
// member's, one without is its member's own.
func visible(c repository.Conversation, caller authn.ConsoleMember) bool {
	return c.SubjectKind != "" || c.MemberID == caller.MemberID
}

func conversationView(c repository.Conversation) Conversation {
	return Conversation{
		ID: c.ID, SubjectKind: domain.SubjectKind(c.SubjectKind), SubjectID: c.SubjectID, Title: c.Title, UpdatedAt: c.LastActivityAt,
	}
}

// Conversations is UC-MOD-08 step 1: the member finds the conversation of a
// record, those about records of a kind, or all their own.
func (s *Agent) Conversations(ctx context.Context, caller authn.ConsoleMember, q ConversationQuery) ([]Conversation, error) {
	if q.SubjectKind == "" && q.SubjectID != nil {
		return nil, domain.ErrInvalidSubject
	}
	rows, err := s.d.Conversations.Conversations(ctx, caller.MemberID,
		repository.ConversationFilter{SubjectKind: string(q.SubjectKind), SubjectID: q.SubjectID}, conversationPage)
	if err != nil {
		return nil, fmt.Errorf("list the conversations: %w", err)
	}
	out := make([]Conversation, 0, len(rows))
	for i := range rows {
		out = append(out, conversationView(rows[i]))
	}
	return out, nil
}

// CreateConversation opens the conversation of a record, which every
// member shares, or starts one of the caller's own.
func (s *Agent) CreateConversation(ctx context.Context, caller authn.ConsoleMember, in NewConversation) (Conversation, error) {
	if (in.SubjectKind == "") != (in.SubjectID == nil) {
		return Conversation{}, domain.ErrInvalidSubject
	}
	now := s.d.Now().UTC()
	c, err := s.d.Conversations.OpenConversation(ctx, repository.Conversation{
		ID: uuid.NewV7(), MemberID: caller.MemberID, SubjectKind: string(in.SubjectKind), SubjectID: in.SubjectID,
		CreatedAt: now, UpdatedAt: now, LastActivityAt: now, RequestKey: in.Key,
	})
	if err != nil {
		return Conversation{}, fmt.Errorf("open the conversation: %w", err)
	}
	return conversationView(c), nil
}

func (s *Agent) visibleConversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (repository.Conversation, error) {
	c, err := s.d.Conversations.Conversation(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && !visible(c, caller)) {
		return repository.Conversation{}, domain.ErrConversationNotFound
	}
	if err != nil {
		return repository.Conversation{}, fmt.Errorf("read the conversation: %w", err)
	}
	return c, nil
}

// Conversation reads a conversation with its history and turns.
func (s *Agent) Conversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConversationDetail, error) {
	c, err := s.visibleConversation(ctx, caller, id)
	if err != nil {
		return ConversationDetail{}, err
	}
	msgs, err := s.d.Conversations.Messages(ctx, id)
	if err != nil {
		return ConversationDetail{}, fmt.Errorf("read the conversation's messages: %w", err)
	}
	turns, err := s.d.Conversations.Turns(ctx, id)
	if err != nil {
		return ConversationDetail{}, fmt.Errorf("read the conversation's turns: %w", err)
	}
	out := ConversationDetail{Conversation: conversationView(c), Messages: make([]Message, 0, len(msgs)), Turns: make([]Turn, 0, len(turns))}
	for _, m := range msgs {
		v := Message{Seq: m.Seq, TurnID: m.TurnID}
		if err = json.Unmarshal(m.Body, &v.Message); err != nil {
			return ConversationDetail{}, fmt.Errorf("read message %d of conversation %s: %w", m.Seq, id, err)
		}
		out.Messages = append(out.Messages, v)
	}
	for i := range turns {
		out.Turns = append(out.Turns, turnView(turns[i]))
	}
	return out, nil
}

// changeable reads a conversation caller may rename or delete: their own,
// or any for an admin; a shared one is an admin's.
func (s *Agent) changeable(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) error {
	c, err := s.d.Conversations.Conversation(ctx, id)
	admin := caller.Role == "admin"
	switch {
	case errors.Is(err, repository.ErrNotFound), err == nil && !admin && c.SubjectKind == "" && c.MemberID != caller.MemberID:
		return domain.ErrConversationNotFound
	case err != nil:
		return fmt.Errorf("read the conversation: %w", err)
	case !admin && c.SubjectKind != "":
		return domain.ErrForbidden
	}
	return nil
}

// RenameConversation gives a conversation a title.
func (s *Agent) RenameConversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, title string) (Conversation, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 80 {
		return Conversation{}, domain.ErrTitleInvalid
	}
	if err := s.changeable(ctx, caller, id); err != nil {
		return Conversation{}, err
	}
	c, err := s.d.Conversations.RenameConversation(ctx, id, title, s.d.Now().UTC())
	if errors.Is(err, repository.ErrNotFound) {
		return Conversation{}, domain.ErrConversationNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("rename the conversation: %w", err)
	}
	return conversationView(c), nil
}

// DeleteConversation deletes a conversation with its messages, turns and
// approvals.
func (s *Agent) DeleteConversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) error {
	if err := s.changeable(ctx, caller, id); err != nil {
		return err
	}
	switch err := s.d.Conversations.DeleteConversation(ctx, id); {
	case errors.Is(err, repository.ErrNotFound):
		return domain.ErrConversationNotFound
	case errors.Is(err, repository.ErrBusy):
		return domain.ErrTurnInProgress
	case err != nil:
		return fmt.Errorf("delete the conversation: %w", err)
	}
	return nil
}
