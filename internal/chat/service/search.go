package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/platform/page"
)

// SearchThreads caps the conversations an inbox search lists.
const SearchThreads = 20

// Search splits q with the index's tokenizer, the last word matching as a
// prefix, reads the threads on the first page and a page of messages, and
// shows each with its other member.
func (c *Chat) Search(ctx context.Context, account uuid.UUID, q string, p page.Request) (SearchResult, error) {
	q = strings.TrimSpace(q)
	if n := utf8.RuneCountInString(q); n == 0 || n > domain.MaxQuery {
		return SearchResult{}, domain.ErrInvalidQuery
	}
	toks, err := c.d.Chats.Tokens(ctx, q)
	if err != nil {
		return SearchResult{}, fmt.Errorf("split query: %w", err)
	}
	if len(toks) == 0 {
		return SearchResult{}, domain.ErrInvalidQuery
	}
	t := repository.Terms{Whole: toks[:len(toks)-1], Prefix: toks[len(toks)-1]}
	var threads []repository.InboxRow
	if p.Cursor == (uuid.UUID{}) {
		if threads, err = c.d.Chats.SearchThreads(ctx, account, t, SearchThreads); err != nil {
			return SearchResult{}, fmt.Errorf("search threads: %w", err)
		}
	}
	hits, err := c.d.Chats.SearchMessages(ctx, account, t, p)
	if err != nil {
		return SearchResult{}, fmt.Errorf("search messages: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(threads)+len(hits))
	for i := range threads {
		ids = append(ids, threads[i].Other)
	}
	for i := range hits {
		ids = append(ids, hits[i].Other)
	}
	others, err := c.d.Accounts.GetMany(ctx, ids)
	if err != nil {
		return SearchResult{}, fmt.Errorf("read the other members: %w", err)
	}
	items := make([]InboxItem, 0, len(threads))
	for i := range threads {
		r := &threads[i]
		items = append(items, InboxItem{
			ID: r.Conversation.ID, Other: PersonFromAccount(r.Other, others[r.Other]), LastMessage: c.messageView(r.Last),
			Unread: r.Unread, Archived: r.Archived,
		})
	}
	found := make([]MessageHit, 0, len(hits))
	for i := range hits {
		h := &hits[i]
		found = append(found, MessageHit{
			ConversationID: h.Message.ConversationID(), Other: PersonFromAccount(h.Other, others[h.Other]),
			Message: c.messageView(h.Message),
		})
	}
	return SearchResult{
		Threads: items, Messages: page.Of(found, p, func(h MessageHit) uuid.UUID { return h.Message.ID }),
	}, nil
}
