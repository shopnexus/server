package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	chatrepo "github.com/shopnexus/server2/internal/chat/repository"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealdomain "github.com/shopnexus/server2/internal/deal/domain"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/media/mediatest"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/support/domain"
	"github.com/shopnexus/server2/internal/support/repository"
	"github.com/shopnexus/server2/internal/support/service"
)

// deals is module deal as support sees it: orders set by the test.
type deals struct {
	mu     *sync.Mutex
	orders map[uuid.UUID]dealsvc.OrderFacts
}

func (d deals) Order(_ context.Context, id uuid.UUID) (dealsvc.OrderFacts, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	f, ok := d.orders[id]
	return f, ok, nil
}

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	chat     *chatsvc.Chat
	support  *service.Support
	deals    deals
}

func newEnv(t *testing.T) env {
	t.Helper()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	accounts := accounttest.Service(db)
	media := mediatest.Service(db, objectstoretest.New(t), in, p)
	catalog := catalogtest.Service(db, accounts, media, in, p)
	chat := chatsvc.New(chatsvc.Deps{
		Chats: chatrepo.NewChats(db), Accounts: accounts, Catalog: catalog, Media: media, Ingress: in, Prefix: p, Now: time.Now,
	})
	d := deals{mu: &sync.Mutex{}, orders: map[uuid.UUID]dealsvc.OrderFacts{}}
	tickets := repository.NewTickets(db)
	s := service.New(service.Deps{
		Tickets: tickets, Accounts: accounts, Chats: chat, Deals: d, Listings: catalog, Ingress: in, Prefix: p, Now: time.Now,
	})
	restatetest.Start(t, append(append(media.Durable(), chat.Durable()...), s.Durable()...)...)
	return env{db: db, accounts: accounts, chat: chat, support: s, deals: d}
}

// order sets an order of buyer's that deal reports with status.
func (e env) order(t *testing.T, buyer uuid.UUID, status dealdomain.OrderStatus, by dealdomain.Party, closed *time.Time) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	seller := accounttest.WithPhone(t, e.db)
	f := dealsvc.OrderFacts{
		BuyerID: buyer, SellerID: seller, ListingID: catalogtest.Listing(t, e.db, seller), Status: status, CompletedBy: by,
		ClosedAt: closed, Price: 3_800_000, Code: "SN-1",
		Shipment: &dealdomain.Shipment{Carrier: dealdomain.CarrierGHTK, TrackingCode: "S1.A1.17373471", Status: dealdomain.ShipmentDelivered},
	}
	// The ticket's order_id references orders, so a row stands behind the facts.
	a := pgx.NamedArgs{"deal": uuid.NewV7(), "order": id, "listing": f.ListingID, "buyer": buyer, "seller": seller}
	for _, q := range []string{
		`INSERT INTO deals (id, listing_id, buyer_id, seller_id, status, listed_price, write_id, created_at, updated_at)
VALUES (@deal, @listing, @buyer, @seller, 'negotiating', 1500000, @deal, now(), now())`,
		`INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name, recipient_phone,
    recipient_street, recipient_commune, shipment_mode, carrier, tracking_code, shipment_status, requested_at, shipped_at)
VALUES (@order, @deal, @listing, @buyer, @seller, 'shipped', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004',
    'manual', 'ghtk', 'S1.A1', 'in_transit', now(), now())`,
	} {
		if _, err := e.db.Exec(context.Background(), q, a); err != nil {
			t.Fatal(err)
		}
	}
	e.deals.mu.Lock()
	defer e.deals.mu.Unlock()
	e.deals.orders[id] = f
	return id
}

// member inserts a console member and returns it as an actor.
func (e env) member(t *testing.T, name string) audit.Actor {
	t.Helper()
	id := uuid.NewV7()
	if _, err := e.db.Exec(context.Background(), `
INSERT INTO console_members (id, email, display_name, console_role, status, password_hash, must_change_password,
                           credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@id, @email, @name, 'moderator', 'active', 'x', false, 1, 0, @at, @at, @id)`,
		pgx.StrictNamedArgs{"id": id, "email": id.String() + "@console.test", "name": name, "at": time.Now()}); err != nil {
		t.Fatal(err)
	}
	return audit.Actor{Kind: audit.ActorMember, MemberID: id}
}

func TestOpenOtherMakesAChatWithTheFirstMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person := accounttest.New(t, e.db)
	tk, existed, err := e.support.Open(ctx, person, uuid.NewV7(), service.OpenInput{Kind: "other", Body: "Lọc khu vực hay nhảy về Q.1"})
	if err != nil || existed || tk.FirstMessageID == nil || tk.Status != domain.StatusOpen {
		t.Fatalf("%+v %v %v", tk, existed, err)
	}
	msgs, err := e.chat.Messages(ctx, person, tk.ConversationID, page.Request{})
	if err != nil || len(msgs.Items) != 1 || msgs.Items[0].Text != "Lọc khu vực hay nhảy về Q.1" || msgs.Items[0].SenderID != person ||
		msgs.Items[0].ID != *tk.FirstMessageID {
		t.Fatalf("%+v %v", msgs.Items, err)
	}
	mine, err := e.support.Mine(ctx, person)
	if err != nil || len(mine) != 1 || mine[0].LastMessage == nil || mine[0].Unread != 0 {
		t.Fatalf("mine %+v %v", mine, err)
	}
	// The row's second line: what the person asked, after ShopNexus answers too.
	if mine[0].FirstMessage == nil || mine[0].FirstMessage.Text != "Lọc khu vực hay nhảy về Q.1" {
		t.Fatalf("first message %+v", mine[0].FirstMessage)
	}
}

func TestReportTwiceReturnsTheTicket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	buyer := accounttest.New(t, e.db)
	order := e.order(t, buyer, dealdomain.OrderShipped, "", nil)
	first, _, err := e.support.Open(ctx, buyer, uuid.NewV7(), service.OpenInput{Kind: "not_received", OrderID: &order})
	if err != nil || first.Order == nil || first.Order.TrackingCode != "S1.A1.17373471" || first.Order.Title == "" {
		t.Fatalf("%+v %v", first, err)
	}
	again, existed, err := e.support.Open(ctx, buyer, uuid.NewV7(), service.OpenInput{Kind: "not_received", OrderID: &order})
	if err != nil || !existed || again.ID != first.ID {
		t.Fatalf("%v %v %v", again.ID, existed, err)
	}
	if got, err := e.support.OfOrder(ctx, buyer, order); err != nil || got.ID != first.ID {
		t.Fatalf("of order %v %v", got.ID, err)
	}
}

func TestReportRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	buyer := accounttest.New(t, e.db)
	old := time.Now().Add(-8 * 24 * time.Hour)
	shipped := e.order(t, buyer, dealdomain.OrderShipped, "", nil)
	late := e.order(t, buyer, dealdomain.OrderCompleted, dealdomain.System, &old)
	for name, c := range map[string]struct {
		account uuid.UUID
		order   uuid.UUID
		want    error
	}{
		"another account's order":            {accounttest.New(t, e.db), shipped, domain.ErrOrderNotFound},
		"completed by the system 8 days ago": {buyer, late, domain.ErrNotAllowed},
		"an unknown order":                   {buyer, uuid.NewV7(), domain.ErrOrderNotFound},
	} {
		if _, _, err := e.support.Open(ctx, c.account, uuid.NewV7(), service.OpenInput{Kind: "not_received", OrderID: &c.order}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSuspendedPersonOpensATicket(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	person := accounttest.New(t, e.db)
	accounttest.Suspend(t, e.accounts, person)
	if _, _, err := e.support.Open(context.Background(), person, uuid.NewV7(), service.OpenInput{Kind: "other", Body: "Vì sao?"}); err != nil {
		t.Fatal(err)
	}
}

func TestReplyGoesOutAsShopNexusAndNamesTheMember(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person := accounttest.New(t, e.db)
	tk, _, err := e.support.Open(ctx, person, uuid.NewV7(), service.OpenInput{Kind: "other", Body: "Hỏi"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := e.support.Reply(ctx, tk.ID, uuid.NewV7().String(), service.ReplyInput{Body: "Chào bạn", ReplyTo: tk.FirstMessageID}, e.member(t, "Lan"))
	if err != nil || m.Message.SenderID != accountdomain.ShopNexusID || m.Member != "Lan" || m.Message.ReplyTo == nil {
		t.Fatalf("%+v %v", m, err)
	}
	d, err := e.support.Detail(ctx, tk.ID)
	if err != nil || len(d.Messages.Items) != 2 || d.Messages.Items[0].Member != "Lan" || d.Messages.Items[1].Member != "" ||
		d.Ticket.LastText != "Chào bạn" {
		t.Fatalf("detail %+v %v", d, err)
	}
	if s, _ := e.support.Summary(ctx, person); s.Unread != 1 || s.Open != 1 || s.LatestUnread == nil {
		t.Fatalf("summary %+v", s)
	}
}

func TestResolvingACarrierRequestSendsTheNote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	seller := accounttest.New(t, e.db)
	tk, _, err := e.support.Open(ctx, seller, uuid.NewV7(), service.OpenInput{Kind: "carrier_request", CarrierName: "Ninja Van"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.support.Resolve(ctx, tk.ID, e.member(t, "Lan")); err != nil {
		t.Fatal(err)
	}
	msgs, err := e.support.Messages(ctx, tk.ID, page.Request{})
	if err != nil || msgs.Items[0].Message.Text != "Đã ghi nhận hãng Ninja Van." || !msgs.Items[0].Automatic {
		t.Fatalf("%+v %v", msgs.Items, err)
	}
	d, _ := e.support.Detail(ctx, tk.ID)
	if d.SameCarrier30d == nil || *d.SameCarrier30d != 1 || d.Ticket.Ticket.Status != domain.StatusResolved {
		t.Fatalf("detail %+v", d)
	}
}

// The person deleted their account: the note cannot go out, and the
// ticket still resolves.
func TestResolvingTheCarrierRequestOfADeletedAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	seller := accounttest.New(t, e.db)
	tk, _, err := e.support.Open(ctx, seller, uuid.NewV7(), service.OpenInput{Kind: "carrier_request", CarrierName: "Ninja Van"})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.accounts.Erase(ctx, seller); err != nil {
		t.Fatal(err)
	}
	got, err := e.support.Resolve(ctx, tk.ID, e.member(t, "Minh"))
	if err != nil || got.Ticket.Status != domain.StatusResolved {
		t.Fatalf("%+v %v", got, err)
	}
}

// A note that fails leaves the ticket open, so "Đã xử lý" can run again;
// before, the status moved first and the next resolve answered 409.
func TestAFailedNoteLeavesTheTicketOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	seller := accounttest.New(t, e.db)
	tk, _, err := e.support.Open(ctx, seller, uuid.NewV7(), service.OpenInput{Kind: "carrier_request", CarrierName: "Ninja Van"})
	if err != nil {
		t.Fatal(err)
	}
	// A block row the app cannot make stands in for a send that fails.
	if _, err = e.db.Exec(ctx, `INSERT INTO account_blocks (id, blocker_id, blocked_id, created_at) VALUES (@id, @p, @s, now())`,
		pgx.StrictNamedArgs{"id": uuid.NewV7(), "p": seller, "s": accountdomain.ShopNexusID}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.support.Resolve(ctx, tk.ID, e.member(t, "Lan")); err == nil {
		t.Fatal("resolved with the note refused")
	}
	if d, _ := e.support.Detail(ctx, tk.ID); d.Ticket.Ticket.Status != domain.StatusOpen {
		t.Fatalf("the ticket resolved without its note: %+v", d.Ticket.Ticket.Status)
	}
}

// The eleventh ticket in a day is refused before its chat opens.
func TestARefusedOpenLeavesNoChat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person := accounttest.New(t, e.db)
	for range domain.DailyLimit {
		if _, _, err := e.support.Open(ctx, person, uuid.NewV7(), service.OpenInput{Kind: "other", Body: "Hỏi"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := e.support.Open(ctx, person, uuid.NewV7(), service.OpenInput{Kind: "other", Body: "Hỏi"}); !errors.Is(err, domain.ErrTooMany) {
		t.Fatalf("the eleventh: %v", err)
	}
	var chats int
	if err := e.db.QueryRow(ctx, `SELECT count(*) FROM conversation_members m JOIN conversations c ON c.id = m.conversation_id
WHERE m.account_id = @p AND c.kind = 'support'`, pgx.StrictNamedArgs{"p": person}).Scan(&chats); err != nil || chats != domain.DailyLimit {
		t.Fatalf("%d chats %v", chats, err)
	}
}

func TestAPersonsMessageReopens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person := accounttest.New(t, e.db)
	tk, _, err := e.support.Open(ctx, person, uuid.NewV7(), service.OpenInput{Kind: "other", Body: "Hỏi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.support.Resolve(ctx, tk.ID, e.member(t, "Lan")); err != nil {
		t.Fatal(err)
	}
	if _, err = e.chat.Send(ctx, person, tk.ConversationID, uuid.NewV7().String(), chatsvc.SendInput{Text: "Còn nữa"}); err != nil {
		t.Fatal(err)
	}
	// The worker calls Messaged on message.sent; worker_test covers that call.
	if err = e.support.Messaged(ctx, tk.ConversationID, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	d, err := e.support.Detail(ctx, tk.ID)
	if err != nil || d.Ticket.Ticket.Status != domain.StatusOpen || d.Ticket.LastFrom != domain.FromPerson {
		t.Fatalf("%+v %v", d.Ticket, err)
	}
	q, err := e.support.Queue(ctx, domain.StatusOpen, nil, page.Request{})
	if err != nil || len(q.Items) != 1 || q.Items[0].Ticket.ID != tk.ID || q.Items[0].Person.ID != person {
		t.Fatalf("queue %+v %v", q, err)
	}
}
