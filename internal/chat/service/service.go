// Package service holds module chat's use cases: a buyer opens a
// conversation from a listing, the two people message each other with text
// and photos, read and archive. Every send runs on Restate, in
// message_send_durable.go, and a photo message finishes in
// message_publish_durable.go.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/chat/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// Service is module chat's contract.
type Service interface {
	// Open starts the buyer's conversation with a listing's seller, or
	// reuses the pair's, and sends the first message about the listing. A
	// repeated idempotencyKey of the same buyer sends nothing more.
	Open(ctx context.Context, buyer uuid.UUID, idempotencyKey string, in OpenInput) (ConversationView, error)
	// Send sends a message in a conversation of the sender's; a repeated
	// idempotencyKey of the same sender sends nothing more.
	Send(ctx context.Context, sender, conversation uuid.UUID, idempotencyKey string, in SendInput) (MessageView, error)
	// Read moves account's read marker to message when message is newer.
	Read(ctx context.Context, account, conversation, message uuid.UUID) error
	Archive(ctx context.Context, account, conversation uuid.UUID) error
	Unarchive(ctx context.Context, account, conversation uuid.UUID) error
	// Inbox lists account's conversations, archived or not, newest message
	// first.
	Inbox(ctx context.Context, account uuid.UUID, archived bool, p page.Request) (page.Page[InboxItem], error)
	// Get reads a conversation of account's.
	Get(ctx context.Context, account, conversation uuid.UUID) (ConversationView, error)
	// ConversationWith returns the ID of account's conversation with other;
	// the zero ID when the two have none.
	ConversationWith(ctx context.Context, account, other uuid.UUID) (uuid.UUID, error)
	// Messages lists what account may read of a conversation, newest first.
	Messages(ctx context.Context, account, conversation uuid.UUID, p page.Request) (page.Page[MessageView], error)
	// Unread counts account's unread messages over unarchived conversations.
	Unread(ctx context.Context, account uuid.UUID) (int, error)
}

// Deps is what New builds Chat from.
type Deps struct {
	Chats    repository.Chats
	Accounts accountsvc.Service
	Auth     authsvc.Service
	Catalog  catalogsvc.Service
	Media    mediasvc.Service
	// Ingress and Prefix reach this process's Restate services; media's
	// MediaProcess runs under the same prefix.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
}

// Chat implements Service in this process.
type Chat struct {
	chats    repository.Chats
	accounts accountsvc.Service
	auth     authsvc.Service
	catalog  catalogsvc.Service
	media    mediasvc.Service
	prefix   string
	now      func() time.Time
	send     restatex.Caller[SendFlowInput, SendResult]
}

var _ Service = (*Chat)(nil)

func New(d Deps) *Chat {
	return &Chat{
		chats: d.Chats, accounts: d.Accounts, auth: d.Auth, catalog: d.Catalog, media: d.Media,
		prefix: d.Prefix, now: d.Now,
		send: restatex.Caller[SendFlowInput, SendResult]{Ingress: d.Ingress, Service: d.Prefix + messageSend, Handler: "Run"},
	}
}

// Durable returns module chat's Restate services, for Mount to bind.
func (c *Chat) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{restate.Reflect(MessageSend{Chat: c}), restate.Reflect(MessagePublish{Chat: c})}
}

// member loads a conversation of account's and account's place in it;
// anyone else gets domain.ErrNotFound, so a conversation's existence does
// not leak.
func (c *Chat) member(ctx context.Context, account, id uuid.UUID) (domain.Conversation, domain.Member, error) {
	conv, err := c.chats.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Conversation{}, domain.Member{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Conversation{}, domain.Member{}, fmt.Errorf("get conversation: %w", err)
	}
	if !conv.Has(account) {
		return domain.Conversation{}, domain.Member{}, domain.ErrNotFound
	}
	m, err := c.chats.Member(ctx, id, account)
	if err != nil {
		return domain.Conversation{}, domain.Member{}, fmt.Errorf("get member: %w", err)
	}
	return conv, m, nil
}
