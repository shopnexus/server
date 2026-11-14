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
	supportsvc "github.com/shopnexus/server2/internal/support/service"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// Service is module screen's contract.
type Service interface {
	// Home reads the Home view around near; viewer is nil when signed out.
	Home(ctx context.Context, viewer *uuid.UUID, near discoverysvc.Near) (HomeScreen, error)
	// Listing reads the listing view of a listing on sale; viewer is nil
	// when signed out.
	Listing(ctx context.Context, viewer *uuid.UUID, id uuid.UUID) (ListingScreen, error)
	// Seller reads the seller view; viewer is nil when signed out.
	Seller(ctx context.Context, viewer *uuid.UUID, id uuid.UUID) (SellerScreen, error)
	// Inbox reads a page of account's inbox view, archived or not.
	Inbox(ctx context.Context, account uuid.UUID, archived bool, p page.Request) (InboxScreen, error)
	// InboxSearch reads the inbox search view: the conversations matching q
	// with their pairs' deals, on the first page only, and a page of
	// matching messages.
	InboxSearch(ctx context.Context, account uuid.UUID, q string, p page.Request) (InboxSearchScreen, error)
	// Chat reads the chat view of a conversation of account's.
	Chat(ctx context.Context, account, conversation uuid.UUID) (ChatScreen, error)
	// ProfileTasks reads account's Hoàn thiện hồ sơ view.
	ProfileTasks(ctx context.Context, account uuid.UUID) (ProfileTasksScreen, error)
	// Reputation reads account's Uy tín của bạn view.
	Reputation(ctx context.Context, account uuid.UUID) (ReputationScreen, error)
}

// Deps is what New builds Screen from.
type Deps struct {
	Accounts accountsvc.Service
	Catalog  catalogsvc.Service
	Deals    dealsvc.Service
	Trust    trustsvc.Service
	Chats    chatsvc.Service
	// Discovery gives the save on the listing view and Home's nearby feed.
	Discovery discoverysvc.Service
	// Notifications gives Home its unread count.
	Notifications notificationsvc.Service
	// Support gives a support chat its ticket.
	Support supportsvc.Service
}

// Screen implements Service in this process.
type Screen struct {
	accounts  accountsvc.Service
	catalog   catalogsvc.Service
	deals     dealsvc.Service
	trust     trustsvc.Service
	chats     chatsvc.Service
	discovery discoverysvc.Service
	notices   notificationsvc.Service
	support   supportsvc.Service
}

var _ Service = (*Screen)(nil)

func New(d Deps) *Screen {
	return &Screen{
		accounts: d.Accounts, catalog: d.Catalog, deals: d.Deals, trust: d.Trust, discovery: d.Discovery, chats: d.Chats, notices: d.Notifications,
		support: d.Support,
	}
}
