// Package service holds module agent's use cases: a console member's
// conversations with the console agent, which the server forwards to the
// agent service, and the record of what the agent read of people through
// the MCP server.
package service

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/authn"
)

// Service is module agent's contract.
type Service interface {
	// Conversations lists the conversations caller may open: the one of
	// the record kind and id name, those about records of kind, or with
	// neither the caller's own.
	//
	// UC-MOD-08
	Conversations(ctx context.Context, caller authn.ConsoleMember, kind domain.SubjectKind, id *uuid.UUID) ([]Conversation, error)
	// CreateConversation returns the conversation of in's subject, shared by
	// every member and made the first time, or starts the caller's own when
	// in has no subject.
	//
	// UC-MOD-08
	CreateConversation(ctx context.Context, caller authn.ConsoleMember, in NewConversation) (Conversation, error)
	// Conversation reads a conversation with its turns.
	//
	// UC-MOD-08
	Conversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConversationDetail, error)
	// Ask puts caller's question to the agent in a conversation and waits
	// for the answer and the sources the agent read.
	//
	// UC-MOD-08 · BR-MOD-03 · ADR-0003
	Ask(ctx context.Context, caller authn.ConsoleMember, conversation uuid.UUID, text string) (Answer, error)
	// RecordRead records that an MCP tool read personal data of subject
	// for member.
	//
	// UC-MOD-08 · NFR-PRIV-02
	RecordRead(ctx context.Context, member uuid.UUID, tool string, kind domain.SubjectKind, ids []uuid.UUID) error
	// SweepReads deletes the reads older than ReadsKeptFor.
	//
	// NFR-PRIV-02
	SweepReads(ctx context.Context) error
}

// ReadsKeptFor is how long a read of personal data stays recorded.
const ReadsKeptFor = 90 * 24 * time.Hour

// Deps is what New builds Agent from.
type Deps struct {
	Reads repository.Reads
	// BaseURL is the agent service's, and Token the bearer token it takes
	// (AGENT_BASE_URL and AGENT_SERVICE_TOKEN).
	BaseURL string
	Token   string
	HTTP    *http.Client
	Now     func() time.Time
}

// Agent implements Service in this process.
type Agent struct {
	reads   repository.Reads
	baseURL string
	token   string
	http    *http.Client
	now     func() time.Time
}

var _ Service = (*Agent)(nil)

func New(d Deps) *Agent {
	return &Agent{reads: d.Reads, baseURL: d.BaseURL, token: d.Token, http: d.HTTP, now: d.Now}
}
