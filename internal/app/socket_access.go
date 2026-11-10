package app

import (
	"context"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/stream"
)

// socketAccess answers the WebSocket's questions from modules account and
// chat.
type socketAccess struct {
	accounts accountsvc.Service
	chats    chatsvc.Service
}

var _ stream.Access = socketAccess{}

// Suspended reports an account that may not keep a socket: one suspended
// or deleted.
func (a socketAccess) Suspended(ctx context.Context, account uuid.UUID) (bool, error) {
	status, err := a.accounts.Status(ctx, account)
	if err != nil {
		return false, fmt.Errorf("check status: %w", err)
	}
	return status != accountdomain.StatusActive, nil
}

func (a socketAccess) Peer(ctx context.Context, account, conversation uuid.UUID) (uuid.UUID, bool, error) {
	other, ok, err := a.chats.Peer(ctx, account, conversation)
	if err != nil {
		return uuid.UUID{}, false, fmt.Errorf("find the peer: %w", err)
	}
	return other, ok, nil
}

func (a socketAccess) Touch(ctx context.Context, account uuid.UUID) error {
	if err := a.accounts.Touch(ctx, account); err != nil {
		return fmt.Errorf("touch: %w", err)
	}
	return nil
}
