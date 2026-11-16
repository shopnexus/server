package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
)

// PostMoves is UC-DEAL-01, step 3: it posts the moves in their order. A
// buyer's or a seller's move is sent by that side's account, the system's
// by ShopNexus; a move of another party is logged and skipped.
func (s *Deals) PostMoves(ctx context.Context, dealID uuid.UUID, ids []uuid.UUID) error {
	d, err := s.deals.Get(ctx, dealID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read deal: %w", err)
	}
	moves, err := s.deals.MovesOf(ctx, dealID, ids)
	if err != nil {
		return fmt.Errorf("read moves: %w", err)
	}
	senders := map[domain.Party]uuid.UUID{
		domain.Buyer: d.BuyerID(), domain.Seller: d.SellerID(), domain.System: accountdomain.ShopNexusID,
	}
	for _, m := range moves {
		if !m.InChat() {
			continue
		}
		sender, ok := senders[m.By]
		if !ok {
			s.log.ErrorContext(ctx, "a move of no party goes to no chat", "deal", dealID, "move", m.ID, "by", m.By)
			continue
		}
		if err = s.chats.PostMove(ctx, chatsvc.MoveInput{
			ID: m.ID, ConversationID: d.ConversationID(), DealID: dealID, SenderID: sender,
			Kind: string(m.Kind), By: string(m.By), Amount: m.Amount, At: m.At,
		}); err != nil {
			return fmt.Errorf("post move %s: %w", m.ID, err)
		}
	}
	return nil
}
