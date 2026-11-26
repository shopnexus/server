package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/chat/chattest"
	chatrepo "github.com/shopnexus/server/internal/chat/repository"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/support/domain"
	"github.com/shopnexus/server/internal/support/repository"
	"github.com/shopnexus/server/internal/support/service"
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
	db *pgxpool.Pool
	// flow runs TicketOpen directly, as a racing retry reaches it.
	flow     ingress.Requester[service.OpenFlowInput, service.OpenResult]
	accounts accountsvc.Service
	chat     *chatsvc.Chat
	support  *service.Support
	deals    deals
}

func newEnv(t *testing.T) env {
	t.Helper()
	return newEnvWithClocks(t, time.Now, time.Now)
}

func newEnvWithClocks(t *testing.T, openedAt, sentAt func() time.Time) env {
	t.Helper()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	accounts := accounttest.Service(db)
	media := mediatest.Service(db, objectstoretest.New(t), in, p)
	catalog := catalogtest.Service(db, accounts, media, in, p)
	chat := chatsvc.New(chatsvc.Deps{
		Chats: chatrepo.NewChats(db), Accounts: accounts, Catalog: catalog, Media: media, Ingress: in, Prefix: p, Now: sentAt,
	})
	d := deals{mu: &sync.Mutex{}, orders: map[uuid.UUID]dealsvc.OrderFacts{}}
	tickets := repository.NewTickets(db)
	s := service.New(service.Deps{
		Tickets: tickets, Accounts: accounts, Chats: chat, Deals: d, Listings: catalog, Ingress: in, Prefix: p, Now: openedAt,
	})
	client := restatetest.Start(t, append(append(media.Durable(), chat.Durable()...), s.Durable()...)...)
	flow := ingress.Service[service.OpenFlowInput, service.OpenResult](client, p+"TicketOpen", "Run")
	return env{db: db, flow: flow, accounts: accounts, chat: chat, support: s, deals: d}
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
	a := pgx.NamedArgs{
		"deal": uuid.NewV7(), "order": id, "listing": f.ListingID, "buyer": buyer, "seller": seller,
		"conversation": chattest.Pair(t, e.db, buyer, seller),
	}
	for _, q := range []string{
		`INSERT INTO deals (id, conversation_id, listing_id, buyer_id, seller_id, status, listed_price, write_id, created_at,
    updated_at)
VALUES (@deal, @conversation, @listing, @buyer, @seller, 'negotiating', 1500000, @deal, now(), now())`,
		`INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name, recipient_phone,
    recipient_street, recipient_commune, recipient_kind, shipment_mode, carrier, tracking_code, shipment_status, requested_at, shipped_at)
VALUES (@order, @deal, @listing, @buyer, @seller, 'shipped', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004', 'home',
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

// UC-MOD-09.
func TestOpenOtherMakesAChatWithTheFirstMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person := accounttest.New(t, e.db)
	tk, existed, err := e.support.Open(ctx, person, uuid.NewV7(), service.OpenInput{Kind: "other", Text: "Lọc khu vực hay nhảy về Q.1"})
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

// UC-MOD-09 A1.
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

// UC-MOD-09 E2, E3.
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

// UC-MOD-09 A4.
func TestSuspendedPersonOpensATicket(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	person := accounttest.New(t, e.db)
	accounttest.Suspend(t, e.accounts, person)
	if _, _, err := e.support.Open(context.Background(), person, uuid.NewV7(), service.OpenInput{Kind: "other", Text: "Vì sao?"}); err != nil {
		t.Fatal(err)
	}
}

// UC-MOD-09.
func TestReplyGoesOutAsShopNexusAndNamesTheMember(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person := accounttest.New(t, e.db)
	tk, _, err := e.support.Open(ctx, person, uuid.NewV7(), service.OpenInput{Kind: "other", Text: "Hỏi"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := e.support.Reply(ctx, tk.ID, uuid.NewV7(), service.ReplyInput{Body: "Chào bạn", ReplyTo: tk.FirstMessageID}, e.member(t, "Lan"))
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
	} // Opening the ticket read its chat for ShopNexus: the person sees "Đã xem" on what came before.
	if c, err := e.chat.Get(ctx, person, tk.ConversationID); err != nil || c.OtherRead == nil || *c.OtherRead != d.Messages.Items[0].Message.ID {
		t.Fatalf("the person's chat after the detail: %+v %v", c, err)
	}
}

// UC-MOD-09 A3.
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
// UC-MOD-09 E1.
func TestARefusedOpenLeavesNoChat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person := accounttest.New(t, e.db)
	for range domain.DailyLimit {
		if _, _, err := e.support.Open(ctx, person, uuid.NewV7(), service.OpenInput{Kind: "other", Text: "Hỏi"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := e.support.Open(ctx, person, uuid.NewV7(), service.OpenInput{Kind: "other", Text: "Hỏi"}); !errors.Is(err, domain.ErrTooMany) {
		t.Fatalf("the eleventh: %v", err)
	}
	var chats int
	if err := e.db.QueryRow(ctx, `SELECT count(*) FROM conversation_members m JOIN conversations c ON c.id = m.conversation_id
WHERE m.account_id = @p AND c.kind = 'support'`, pgx.StrictNamedArgs{"p": person}).Scan(&chats); err != nil || chats != domain.DailyLimit {
		t.Fatalf("%d chats %v", chats, err)
	}
}

// supportChats counts person's support chats.
func (e env) supportChats(t *testing.T, person uuid.UUID) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM conversation_members m
JOIN conversations c ON c.id = m.conversation_id WHERE m.account_id = @p AND c.kind = 'support'`,
		pgx.StrictNamedArgs{"p": person}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A first message that chat refuses leaves no ticket and no chat, and the
// key's retry gets the refusal again. UC-MOD-09 E5.
func TestARefusedFirstMessageLeavesNoTicket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person, key := accounttest.New(t, e.db), uuid.NewV7()
	in := service.OpenInput{Kind: "other", Photos: []uuid.UUID{uuid.NewV7()}}
	for i := range 2 {
		if _, _, err := e.support.Open(ctx, person, key, in); err == nil || !strings.Contains(err.Error(), "invalid_upload") {
			t.Fatalf("open %d: %v", i, err)
		}
	}
	mine, err := e.support.Mine(ctx, person)
	if err != nil || len(mine) != 0 || e.supportChats(t, person) != 0 {
		t.Fatalf("left %d tickets, %d chats, %v", len(mine), e.supportChats(t, person), err)
	}
}

// A run that finds the key's ticket from another run drops the chat it opened.
func TestARunThatLosesTheKeyDropsItsChat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person, key := accounttest.New(t, e.db), uuid.NewV7()
	tk, _, err := e.support.Open(ctx, person, key, service.OpenInput{Kind: "other", Text: "Hỏi"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.flow.Request(ctx, service.OpenFlowInput{Account: person, Key: key, Input: service.OpenInput{Kind: "other", Text: "Hỏi"}})
	if err != nil || res.Refused != nil || res.TicketID != tk.ID || e.supportChats(t, person) != 1 {
		t.Fatalf("%+v %v, %d chats", res, err, e.supportChats(t, person))
	}
}

// UC-MOD-09 A2.
func TestAPersonsMessageReopens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person := accounttest.New(t, e.db)
	tk, _, err := e.support.Open(ctx, person, uuid.NewV7(), service.OpenInput{Kind: "other", Text: "Hỏi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.support.Resolve(ctx, tk.ID, e.member(t, "Lan")); err != nil {
		t.Fatal(err)
	}
	if _, err = e.chat.Send(ctx, person, tk.ConversationID, uuid.NewV7(), chatsvc.SendInput{Text: "Còn nữa"}); err != nil {
		t.Fatal(err)
	}
	// The worker calls Messaged on message.sent; worker_test covers that call.
	if err = e.support.Messaged(ctx, tk.ConversationID, uuid.NewV7(), true, time.Now()); err != nil {
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

func TestDurableInitialMessageUsesItsSendTime(t *testing.T) {
	t.Parallel()
	openedAt := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	sentAt := openedAt.Add(time.Hour)
	e := newEnvWithClocks(t, func() time.Time { return openedAt }, func() time.Time { return sentAt })
	owner := accounttest.New(t, e.db)
	tk, _, err := e.support.Open(t.Context(), owner, uuid.NewV7(), service.OpenInput{Kind: "other", Text: "Hỏi"})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.NewTickets(e.db).Get(t.Context(), tk.ID)
	if err != nil || !stored.OpenedAt.Equal(openedAt) || !stored.LastMessageAt.Equal(sentAt) {
		t.Fatalf("delayed durable first message: %+v %v", stored, err)
	}
}

func TestAccountDeletedKeepsDelayedFirstDeliveryResolved(t *testing.T) {
	t.Parallel()
	openedAt := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	clock := restatetest.NewClock(openedAt.Add(time.Minute))
	e := newEnvWithClocks(t, func() time.Time { return openedAt }, clock.Now)
	owner := accounttest.New(t, e.db)
	tk, _, err := e.support.Open(t.Context(), owner, uuid.NewV7(), service.OpenInput{Kind: "other", Text: "Hỏi"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Minute)
	msg, err := e.chat.Send(t.Context(), owner, tk.ConversationID, uuid.NewV7(), chatsvc.SendInput{Text: "Còn nữa"})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.accounts.Erase(t.Context(), owner); err != nil {
		t.Fatal(err)
	}
	resolvedAt := openedAt.Add(5 * time.Minute)
	if err = e.support.AccountDeleted(t.Context(), owner, uuid.NewV7(), resolvedAt); err != nil {
		t.Fatal(err)
	}
	if err = e.support.Messaged(t.Context(), tk.ConversationID, msg.ID, true, clock.Now()); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.NewTickets(e.db).Get(t.Context(), tk.ID)
	if err != nil || stored.Status != domain.StatusResolved || stored.ResolvedAt == nil || !stored.ResolvedAt.Equal(resolvedAt) {
		t.Fatalf("late first delivery after AccountDeleted: %+v %v", stored, err)
	}
	if err = e.support.Messaged(t.Context(), tk.ConversationID, uuid.NewV7(), true, resolvedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	stored, err = repository.NewTickets(e.db).Get(t.Context(), tk.ID)
	if err != nil || stored.Status != domain.StatusResolved {
		t.Fatalf("deleted owner reopened: %+v %v", stored, err)
	}
}
