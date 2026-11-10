// Package service holds module support's use cases: a person sends
// feedback, and console members read it.
package service

import (
	"context"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/support/repository"
)

// Service is module support's contract.
type Service interface {
	// Send keeps a feedback of the account's under the request's key; a
	// repeated key returns the first one.
	Send(ctx context.Context, account, key uuid.UUID, body string) (FeedbackView, error)
	// Feedback lists every feedback, newest first, for console members.
	Feedback(ctx context.Context, p page.Request) (page.Page[ConsoleFeedback], error)
}

// FeedbackView is the Feedback schema.
type FeedbackView struct {
	ID        uuid.UUID `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// ConsoleFeedback is the ConsoleFeedback schema.
type ConsoleFeedback struct {
	ID        uuid.UUID       `json:"id"`
	Body      string          `json:"body"`
	CreatedAt time.Time       `json:"created_at"`
	Account   FeedbackAccount `json:"account"`
}

// FeedbackAccount is who sent a feedback.
type FeedbackAccount struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Deleted     bool      `json:"deleted"`
}

// Deps is what New builds Support from.
type Deps struct {
	Feedback repository.Feedback
	Accounts accountsvc.Service
	Now      func() time.Time
}

// Support implements Service in this process.
type Support struct {
	feedback repository.Feedback
	accounts accountsvc.Service
	now      func() time.Time
}

var _ Service = (*Support)(nil)

func New(d Deps) *Support { return &Support{feedback: d.Feedback, accounts: d.Accounts, now: d.Now} }
