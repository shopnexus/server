package service

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/platform/authn"
)

// Conversations is UC-MOD-08 step 1: the member finds the conversation of a
// record, those about records of a kind, or their own.
func (s *Agent) Conversations(ctx context.Context, caller authn.ConsoleMember, kind domain.SubjectKind, id *uuid.UUID) ([]Conversation, error) {
	q := url.Values{}
	switch {
	case id != nil && kind == "":
		return nil, domain.ErrInvalidSubject
	case id != nil:
		q.Set("subject_kind", string(kind))
		q.Set("subject_id", id.String())
	case kind != "":
		q.Set("subject_kind", string(kind))
	default:
	}
	var got []Conversation
	if err := s.call(ctx, caller, http.MethodGet, "/conversations", q, nil, &got, readTimeout, domain.ErrUnavailable); err != nil {
		return nil, err
	}
	if got == nil {
		got = []Conversation{}
	}
	return got, nil
}

// CreateConversation is UC-MOD-08 step 1: the member opens the record's
// conversation, which every member shares and the agent makes the first
// time, or a new one of their own.
func (s *Agent) CreateConversation(ctx context.Context, caller authn.ConsoleMember, in NewConversation) (Conversation, error) {
	if (in.SubjectKind == "") != (in.SubjectID == nil) {
		return Conversation{}, domain.ErrInvalidSubject
	}
	var got Conversation
	if err := s.call(ctx, caller, http.MethodPost, "/conversations", nil, in, &got, readTimeout, domain.ErrUnavailable); err != nil {
		return Conversation{}, err
	}
	return got, nil
}

// Conversation is UC-MOD-08 step 1: the member reads a conversation, each
// question with the member who asked it.
func (s *Agent) Conversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConversationDetail, error) {
	var got ConversationDetail
	if err := s.call(ctx, caller, http.MethodGet, "/conversations/"+id.String(), nil, nil, &got, readTimeout, domain.ErrUnavailable); err != nil {
		return ConversationDetail{}, err
	}
	turns := make([]Turn, 0, len(got.Turns))
	for i := range got.Turns {
		t := got.Turns[i]
		t.Status, t.Sources = strings.ToLower(t.Status), sourcesOf(t.Sources)
		turns = append(turns, t)
	}
	got.Turns = turns
	return got, nil
}
