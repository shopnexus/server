// Package service holds module screen's views: each reads what one view of
// the app shows from the other modules' services.
package service

import (
	"context"
	"uuid"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	discoverysvc "github.com/shopnexus/server2/internal/discovery/service"
	notificationsvc "github.com/shopnexus/server2/internal/notification/service"
	"github.com/shopnexus/server2/internal/platform/page"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// Service is module screen's contract.
type Service interface {
	// Home reads the Home view around near; reader is nil for a signed-out
	// reader.
	Home(ctx context.Context, reader *uuid.UUID, near discoverysvc.Near) (HomeScreen, error)
	// Listing reads the listing view of a listing on sale; reader is nil
	// for a signed-out reader.
	Listing(ctx context.Context, reader *uuid.UUID, id uuid.UUID) (ListingScreen, error)
	// Seller reads the seller view; reader is nil for a signed-out reader.
	Seller(ctx context.Context, reader *uuid.UUID, id uuid.UUID) (SellerScreen, error)
	// Inbox reads a page of account's inbox view, archived or not.
	Inbox(ctx context.Context, account uuid.UUID, archived bool, p page.Request) (InboxScreen, error)
	// Chat reads the chat view of a conversation of account's.
	Chat(ctx context.Context, account, conversation uuid.UUID) (ChatScreen, error)
}

// Deps is what New builds Screen from.
type Deps struct {
	Accounts accountsvc.Service
	Catalog  catalogsvc.Service
	Deals    dealsvc.Service
	Trust    trustsvc.Service
	Saves    discoverysvc.Service
	Chats    chatsvc.Service
	// Notifications gives Home its unread count.
	Notifications notificationsvc.Service
}

// Screen implements Service in this process.
type Screen struct {
	accounts accountsvc.Service
	catalog  catalogsvc.Service
	deals    dealsvc.Service
	trust    trustsvc.Service
	saves    discoverysvc.Service
	chats    chatsvc.Service
	notices  notificationsvc.Service
}

var _ Service = (*Screen)(nil)

func New(d Deps) *Screen {
	return &Screen{
		accounts: d.Accounts, catalog: d.Catalog, deals: d.Deals, trust: d.Trust, saves: d.Saves, chats: d.Chats, notices: d.Notifications,
	}
}
