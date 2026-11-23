// Package service holds module screen's views: each reads what one view of
// the app shows from the other modules' services.
package service

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	notificationsvc "github.com/shopnexus/server/internal/notification/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
	shippingsvc "github.com/shopnexus/server/internal/shipping/service"
	supportsvc "github.com/shopnexus/server/internal/support/service"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// Service is module screen's contract.
type Service interface {
	Order(ctx context.Context, account, order uuid.UUID) (OrderScreen, error)
	// Orders reads a page of account's orders on side as, each with the
	// other party and, for the buyer, account's review, and the side's
	// count: the buyer's open COD orders, the seller's pending orders.
	Orders(ctx context.Context, account uuid.UUID, as dealdomain.Party, p page.Request) (OrdersScreen, error)
	// MyListings reads a page of one of the seller's panes of Tin của bạn,
	// each listing with its views, askers, saves and open order, and the
	// last seven days of the listings on sale.
	MyListings(ctx context.Context, seller uuid.UUID, pane catalogsvc.Pane, p page.Request, t locale.Tag) (MyListingsScreen, error)
	// Offers reads a page of the buyers' open offers on seller's listings,
	// newest offer first, each with its buyer.
	Offers(ctx context.Context, seller uuid.UUID, p page.Request) (OffersScreen, error)
	CodSetup(ctx context.Context, account uuid.UUID) (CodSetupScreen, error)
	CodCheckout(ctx context.Context, account, deal uuid.UUID, address *uuid.UUID) (CodCheckoutScreen, error)
	// Home reads the Home view around near, within radiusKm when it is not
	// 0; viewer is nil when signed out.
	Home(ctx context.Context, viewer *uuid.UUID, near catalogdomain.Near, radiusKm int, t locale.Tag) (HomeScreen, error)
	// Listing reads the listing view of a listing on sale, with its
	// distance from in.Near; viewer is nil when signed out. A signed-in
	// viewer who is not the seller counts a view of it, unless the read is
	// a prefetch.
	Listing(ctx context.Context, viewer *uuid.UUID, in ListingRead) (ListingScreen, error)
	// SoldListing reads the sold listing view of a listing that sold, for
	// anyone; ref is its ID or slug.
	SoldListing(ctx context.Context, ref string) (SoldListingScreen, error)
	// Seller reads the seller view, its listings as search items measured
	// from near; viewer is nil when signed out.
	Seller(ctx context.Context, viewer *uuid.UUID, id uuid.UUID, near catalogdomain.Near) (SellerScreen, error)
	// Inbox reads a page of account's inbox view, archived or not, scam
	// signals written in t.
	Inbox(ctx context.Context, account uuid.UUID, archived bool, p page.Request, t locale.Tag) (InboxScreen, error)
	// InboxSearch reads the inbox search view: the conversations matching q
	// with their pairs' deals, on the first page only, and a page of
	// matching messages. Scam signals are written in t.
	InboxSearch(ctx context.Context, account uuid.UUID, q string, p page.Request, t locale.Tag) (InboxSearchScreen, error)
	// Chat reads the chat view of a conversation of account's, scam
	// signals written in t.
	Chat(ctx context.Context, account, conversation uuid.UUID, t locale.Tag) (ChatScreen, error)
	// ProfileTasks reads account's Hoàn thiện hồ sơ view.
	ProfileTasks(ctx context.Context, account uuid.UUID) (ProfileTasksScreen, error)
	// Reputation reads account's Uy tín của bạn view.
	Reputation(ctx context.Context, account uuid.UUID) (ReputationScreen, error)
	// Unread counts account's unread messages, as the inbox does, its
	// unread notices and the buyers' open offers waiting on it.
	Unread(ctx context.Context, account uuid.UUID) (Unread, error)
}

// Deps is what New builds Screen from.
type Deps struct {
	Shipping shippingsvc.Service
	Accounts accountsvc.Service
	Catalog  catalogsvc.Service
	Deals    dealsvc.Service
	Trust    trustsvc.Service
	Chats    chatsvc.Service
	// Discovery gives the save on the listing view, Home's nearby feed,
	// the seller view's listings and the views and saves of Tin của bạn.
	Discovery discoverysvc.Service
	// Notifications gives Home and GET /v1/me/unread the unread notices.
	Notifications notificationsvc.Service
	// Support gives a support chat its ticket.
	Support supportsvc.Service
	Now     func() time.Time
	Log     *slog.Logger
}

// Screen implements Service in this process.
type Screen struct {
	shipping  shippingsvc.Service
	accounts  accountsvc.Service
	catalog   catalogsvc.Service
	deals     dealsvc.Service
	trust     trustsvc.Service
	chats     chatsvc.Service
	discovery discoverysvc.Service
	notices   notificationsvc.Service
	support   supportsvc.Service
	now       func() time.Time
	log       *slog.Logger
}

var _ Service = (*Screen)(nil)

func New(d Deps) *Screen {
	return &Screen{
		accounts: d.Accounts, catalog: d.Catalog, deals: d.Deals, trust: d.Trust, discovery: d.Discovery, chats: d.Chats, notices: d.Notifications,
		support: d.Support, shipping: d.Shipping, now: d.Now, log: d.Log,
	}
}
