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

	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/restatex"
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
	// Read moves account's read marker to message when message is newer,
	// and clears account's unread mark.
	Read(ctx context.Context, account, conversation, message uuid.UUID) error
	// MarkUnread marks a conversation of account's unread until account
	// reads it; the read marker stays.
	MarkUnread(ctx context.Context, account, conversation uuid.UUID) error
	Archive(ctx context.Context, account, conversation uuid.UUID) error
	Unarchive(ctx context.Context, account, conversation uuid.UUID) error
	// Inbox lists account's conversations, archived or not, newest message
	// first.
	Inbox(ctx context.Context, account uuid.UUID, archived bool, p page.Request) (page.Page[InboxItem], error)
	// Get reads a conversation of account's, with the other member's read
	// marker.
	Get(ctx context.Context, account, conversation uuid.UUID) (ConversationView, error)
	// ConversationWith returns the ID of account's conversation with other;
	// the zero ID when the two have none.
	ConversationWith(ctx context.Context, account, other uuid.UUID) (uuid.UUID, error)
	// OpenPair returns the ID of the conversation of one and other, opening
	// it when the two have none; opened reports a new one. Module deal opens
	// it with a deal and has checked the two may deal.
	OpenPair(ctx context.Context, one, other uuid.UUID) (id uuid.UUID, opened bool, err error)
	// DropPair deletes a pair conversation that OpenPair opened while no
	// message, deal or other row refers to it; one in use stays. Module
	// deal drops the conversation it opened for a deal it could not store.
	DropPair(ctx context.Context, id uuid.UUID) error
	// PostMove writes the deal message of a move into the pair's
	// conversation. Posting the same move again writes nothing more; a
	// move that does not belong there is domain.ErrMisplacedMove, which no
	// retry fixes. Module deal only.
	PostMove(ctx context.Context, in MoveInput) error
	// Peer returns the other member of conversation when account is a
	// member and neither blocks the other; ok is false otherwise, and for an
	// unknown conversation.
	Peer(ctx context.Context, account, conversation uuid.UUID) (other uuid.UUID, ok bool, err error)
	// Messages lists what account may read of a conversation, newest first.
	Messages(ctx context.Context, account, conversation uuid.UUID, p page.Request) (page.Page[MessageView], error)
	// OpenSupport makes the support chat id between account and ShopNexus,
	// or returns it when a retried step made it already. Module support
	// only.
	OpenSupport(ctx context.Context, account, id uuid.UUID) (ConversationView, error)
	// Threads reads the last message and account's unread count of each of
	// account's conversations among ids; one with no message is absent.
	Threads(ctx context.Context, account uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Thread, error)
	// SupportUnread counts account's unread messages over its support
	// chats, which Unread leaves out.
	SupportUnread(ctx context.Context, account uuid.UUID) (int, error)
	// MessageFacts reads a message for its push; false for an unknown one.
	MessageFacts(ctx context.Context, id uuid.UUID) (MessageFacts, bool, error)
	// Window reads conversation's last n sent messages with text up to
	// message upTo, or the newest for the zero upTo, oldest first; false for
	// an unknown conversation.
	Window(ctx context.Context, conversation, upTo uuid.UUID, n int) (Window, bool, error)
	// Unread counts account's unread messages over unarchived pair
	// conversations; support chats are left out.
	Unread(ctx context.Context, account uuid.UUID) (int, error)
	// ReplyRate is how often account answered within domain.ReplyWindow
	// the conversations others started about its listings over
	// domain.ReplyPeriod; nil while fewer than domain.MinReplyCount count.
	ReplyRate(ctx context.Context, account uuid.UUID) (*ReplyRate, error)
	// ListingAskers reads, for each of the seller's listings among ids, the
	// people who wrote to the seller about it, with their first such
	// message's time; a listing nobody wrote about has no entry.
	ListingAskers(ctx context.Context, seller uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]ListingAsker, error)
	// Search finds account's conversations by the other member's name or a
	// listing's title, on the first page only, and a page of their messages
	// by text; archived conversations are searched too.
	Search(ctx context.Context, account uuid.UUID, q string, p page.Request) (SearchResult, error)
}

// Deps is what New builds Chat from.
type Deps struct {
	Chats    repository.Chats
	Accounts accountsvc.Service
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
	catalog  catalogsvc.Service
	media    mediasvc.Service
	prefix   string
	now      func() time.Time
	send     restatex.Caller[SendFlowInput, SendResult]
}

var _ Service = (*Chat)(nil)

func New(d Deps) *Chat {
	return &Chat{
		chats: d.Chats, accounts: d.Accounts, catalog: d.Catalog, media: d.Media,
		prefix: d.Prefix, now: d.Now,
		send: restatex.Caller[SendFlowInput, SendResult]{Ingress: d.Ingress, Service: d.Prefix + messageSend, Handler: "Run"},
	}
}

// Durable returns module chat's Restate services, for Mount to bind.
func (c *Chat) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{restate.Reflect(MessageSend{Chat: c}), restate.Reflect(MessagePublish{Chat: c})}
}

// member loads a conversation of account's; anyone else gets
// domain.ErrNotFound, so a conversation's existence does not leak.
func (c *Chat) member(ctx context.Context, account, id uuid.UUID) (domain.Conversation, error) {
	conv, err := c.chats.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && !conv.Has(account)) {
		return domain.Conversation{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	return conv, nil
}
