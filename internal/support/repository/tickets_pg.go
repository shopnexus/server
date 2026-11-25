package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/platform/stream"
	"github.com/shopnexus/server/internal/support/domain"
)

type pgTickets struct{ db *pgxpool.Pool }

func NewTickets(db *pgxpool.Pool) Tickets { return pgTickets{db} }

const ticketColumns = `t.id, t.account_id, t.conversation_id, t.kind, t.status, t.order_id, t.seller_id, coalesce(t.carrier_name, ''),
       t.first_message_id, t.last_message_at, t.last_from, t.opened_at, t.resolved_at, t.resolved_by`

func scanTicket(row pgx.CollectableRow) (Ticket, error) {
	var t Ticket
	if err := row.Scan(&t.ID, &t.AccountID, &t.ConversationID, &t.Kind, &t.Status, &t.OrderID, &t.SellerID, &t.CarrierName,
		&t.FirstMessageID, &t.LastMessageAt, &t.LastFrom, &t.OpenedAt, &t.ResolvedAt, &t.ResolvedBy); err != nil {
		return Ticket{}, fmt.Errorf("scan ticket: %w", err)
	}
	return t, nil
}

// batcher is a pool or a transaction.
type batcher interface {
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// queueRead queues into b the tickets where selects and their events; out
// holds them once b is sent. The events' statement selects the tickets
// again, so it can miss or add one committed in between; it skips those.
func queueRead(b *pgx.Batch, where string, args pgx.StrictNamedArgs, out *[]Ticket) {
	at := map[uuid.UUID]int{}
	b.Queue(`SELECT `+ticketColumns+` FROM tickets t `+where, args).Query(func(rows pgx.Rows) error {
		ts, err := pgx.CollectRows(rows, scanTicket)
		if err != nil {
			return fmt.Errorf("read tickets: %w", err)
		}
		for i := range ts {
			at[ts[i].ID] = i
		}
		*out = ts
		return nil
	})
	b.Queue(`SELECT e.ticket_id, e.kind, e.member_id, e.at FROM ticket_events e
WHERE e.ticket_id IN (SELECT t.id FROM tickets t `+where+`) ORDER BY e.at, e.id`, args).Query(func(rows pgx.Rows) error {
		_, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct{}, error) {
			var (
				id uuid.UUID
				e  Event
			)
			if scanErr := row.Scan(&id, &e.Kind, &e.MemberID, &e.At); scanErr != nil {
				return struct{}{}, fmt.Errorf("scan event: %w", scanErr)
			}
			if i, ok := at[id]; ok {
				(*out)[i].Events = append((*out)[i].Events, e)
			}
			return struct{}{}, nil
		})
		if err != nil {
			return fmt.Errorf("read events: %w", err)
		}
		return nil
	})
}

// read selects tickets with where and loads their events, in one batch.
func read(ctx context.Context, q batcher, where string, args pgx.StrictNamedArgs) ([]Ticket, error) {
	var ts []Ticket
	b := &pgx.Batch{}
	queueRead(b, where, args, &ts)
	if err := q.SendBatch(ctx, b).Close(); err != nil {
		return nil, fmt.Errorf("select tickets: %w", err)
	}
	return ts, nil
}

// one reads the single ticket where matches; domain.ErrNotFound for none.
func one(ctx context.Context, q batcher, where string, args pgx.StrictNamedArgs) (Ticket, error) {
	ts, err := read(ctx, q, where, args)
	if err != nil {
		return Ticket{}, err
	}
	if len(ts) == 0 {
		return Ticket{}, domain.ErrNotFound
	}
	return ts[0], nil
}

// queueEvent queues into b the insert of ticket event id, which the caller
// chose.
func queueEvent(b *pgx.Batch, id, ticket uuid.UUID, kind string, member *uuid.UUID, at time.Time) {
	b.Queue(`INSERT INTO ticket_events (id, ticket_id, kind, member_id, at) VALUES (@id, @ticket, @kind, @member, @at)`,
		pgx.StrictNamedArgs{"id": id, "ticket": ticket, "kind": kind, "member": member, "at": at}).Fn = func(br pgx.BatchResults) error {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert the %s event: %w", kind, err)
		}
		return nil
	}
}

// Insert reads the account's prior ticket and count under the account's
// lock in one batch, then writes the ticket, its event and update and
// reads it back in another.
func (s pgTickets) Insert(ctx context.Context, t Ticket, key uuid.UUID, carrierKey string, since time.Time) (Ticket, bool, error) {
	var (
		out     Ticket
		existed bool
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			ofKey, ofOrder []Ticket
			n              int
		)
		b := &pgx.Batch{}
		postgres.QueueLock(b, "tickets", t.AccountID)
		queueRead(b, `WHERE t.account_id = @account AND t.request_key = @key`,
			pgx.StrictNamedArgs{"account": t.AccountID, "key": key}, &ofKey)
		queueRead(b, `WHERE t.account_id = @account AND t.kind = 'not_received' AND t.order_id = @order`,
			pgx.StrictNamedArgs{"account": t.AccountID, "order": t.OrderID}, &ofOrder)
		b.Queue(`SELECT count(*) FROM tickets WHERE account_id = @account AND opened_at > @since`,
			pgx.StrictNamedArgs{"account": t.AccountID, "since": since}).QueryRow(func(row pgx.Row) error {
			if err := row.Scan(&n); err != nil {
				return fmt.Errorf("count tickets: %w", err)
			}
			return nil
		})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("read prior tickets: %w", err)
		}
		// The key's ticket is this request's, from a retried step or a
		// racing retry; the order's is another request's.
		if len(ofKey) > 0 {
			out = ofKey[0]
			return nil
		}
		if len(ofOrder) > 0 {
			out, existed = ofOrder[0], true
			return nil
		}
		if n >= domain.DailyLimit {
			return domain.ErrTooMany
		}
		var carrier, carrierKeyArg any
		if t.CarrierName != "" {
			carrier, carrierKeyArg = t.CarrierName, carrierKey
		}
		var made []Ticket
		b = &pgx.Batch{}
		b.Queue(`INSERT INTO tickets (id, account_id, conversation_id, kind, status, order_id, seller_id, carrier_name,
    carrier_key, last_message_at, last_from, opened_at, request_key)
VALUES (@id, @account, @conversation, @kind, 'open', @order, @seller, @carrier, @carrier_key, @at, 'person', @at, @key)`,
			pgx.StrictNamedArgs{
				"id": t.ID, "account": t.AccountID, "conversation": t.ConversationID, "kind": t.Kind, "order": t.OrderID,
				"seller":  t.SellerID,
				"carrier": carrier, "carrier_key": carrierKeyArg, "at": t.OpenedAt, "key": key,
			})
		// A ticket opens once, so its opened event takes the ticket's ID.
		queueEvent(b, t.ID, t.ID, "opened", nil, t.OpenedAt)
		stream.Queue(b, []uuid.UUID{t.AccountID}, stream.Hint{Kind: stream.KindTicket, ConversationID: &t.ConversationID})
		queueRead(b, `WHERE t.id = @id`, pgx.StrictNamedArgs{"id": t.ID}, &made)
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("write ticket: %w", err)
		}
		out = made[0]
		return nil
	})
	if err != nil {
		return Ticket{}, false, fmt.Errorf("insert ticket: %w", err)
	}
	return out, existed, nil
}

func (s pgTickets) SetFirstMessage(ctx context.Context, id, message uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			t      Ticket
			sentAt time.Time
			sent   bool
		)
		// A photo message is still processing here; it is the card all the same.
		err := tx.QueryRow(ctx, `UPDATE tickets t SET first_message_id = m.id
FROM messages m
WHERE t.id = @id AND t.first_message_id IS NULL AND m.id = @message
  AND m.conversation_id = t.conversation_id AND m.sender_id = t.account_id
RETURNING t.id, t.account_id, t.conversation_id, t.status, t.resolved_at, m.created_at, m.status = 'sent'`,
			pgx.StrictNamedArgs{"id": id, "message": message}).Scan(&t.ID, &t.AccountID, &t.ConversationID, &t.Status, &t.ResolvedAt, &sentAt, &sent)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("set first message: %w", err)
		}
		b := &pgx.Batch{}
		// One not sent yet is recorded by the worker's Messaged once it is.
		if sent {
			queueMessage(b, t, message, domain.FromPerson, sentAt, nil)
		}
		stream.Queue(b, []uuid.UUID{t.AccountID}, stream.Hint{Kind: stream.KindTicket, ConversationID: &t.ConversationID})
		if err = tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("record the first message: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("attach first message: %w", err)
	}
	return nil
}

func (s pgTickets) OpenedSince(ctx context.Context, account uuid.UUID, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM tickets WHERE account_id = @account AND opened_at > @since`,
		pgx.StrictNamedArgs{"account": account, "since": since}).Scan(&n); err != nil {
		return 0, fmt.Errorf("count tickets: %w", err)
	}
	return n, nil
}

func (s pgTickets) Get(ctx context.Context, id uuid.UUID) (Ticket, error) {
	return one(ctx, s.db, `WHERE t.id = @id`, pgx.StrictNamedArgs{"id": id})
}

func (s pgTickets) ByConversation(ctx context.Context, conversation uuid.UUID) (Ticket, error) {
	return one(ctx, s.db, `WHERE t.conversation_id = @conversation`, pgx.StrictNamedArgs{"conversation": conversation})
}

func (s pgTickets) ByKey(ctx context.Context, account, key uuid.UUID) (Ticket, error) {
	return one(ctx, s.db, `WHERE t.account_id = @account AND t.request_key = @key`,
		pgx.StrictNamedArgs{"account": account, "key": key})
}

func (s pgTickets) ByOrder(ctx context.Context, account, order uuid.UUID) (Ticket, error) {
	return one(ctx, s.db, `WHERE t.account_id = @account AND t.kind = 'not_received' AND t.order_id = @order`,
		pgx.StrictNamedArgs{"account": account, "order": order})
}

func (s pgTickets) OfAccount(ctx context.Context, account uuid.UUID) ([]Ticket, error) {
	return read(ctx, s.db, `WHERE t.account_id = @account ORDER BY t.status = 'resolved', t.last_message_at DESC, t.id DESC`,
		pgx.StrictNamedArgs{"account": account})
}

func (s pgTickets) Involving(ctx context.Context, account uuid.UUID, limit int) ([]Ticket, error) {
	return read(ctx, s.db, `WHERE t.account_id = @account OR t.seller_id = @account ORDER BY t.opened_at DESC, t.id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"account": account, "limit": limit})
}

func (s pgTickets) Queue(ctx context.Context, status domain.Status, kind *domain.Kind, p page.Request) ([]Ticket, error) {
	return read(ctx, s.db, `WHERE t.status = @status AND (@kind::text IS NULL OR t.kind = @kind)
  AND (@cursor::uuid IS NULL OR (t.last_message_at, t.id) < (SELECT c.last_message_at, c.id FROM tickets c WHERE c.id = @cursor))
ORDER BY t.last_message_at DESC, t.id DESC LIMIT @limit`, pgx.StrictNamedArgs{
		"status": status, "kind": kind, "cursor": cursorOf(p), "limit": p.Fetch(),
	})
}

// cursorOf is the request's cursor, or NULL for the first page.
func cursorOf(p page.Request) *uuid.UUID {
	if p.Cursor == (uuid.UUID{}) {
		return nil
	}
	return &p.Cursor
}

func (s pgTickets) CountOpen(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM tickets WHERE status = 'open'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count open tickets: %w", err)
	}
	return n, nil
}

func (s pgTickets) CountByStatus(ctx context.Context, since time.Time) (map[domain.Status]int, error) {
	return postgres.Counts[domain.Status](s.db.Query(ctx, `SELECT status, count(*) FROM tickets WHERE opened_at >= @since GROUP BY status`,
		pgx.StrictNamedArgs{"since": since}))
}

func (s pgTickets) CountOpenOf(ctx context.Context, account uuid.UUID) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM tickets WHERE account_id = @account AND status = 'open'`,
		pgx.StrictNamedArgs{"account": account}).Scan(&n); err != nil {
		return 0, fmt.Errorf("count open tickets: %w", err)
	}
	return n, nil
}

func (s pgTickets) SameCarrier(ctx context.Context, carrierKey string, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM tickets
WHERE kind = 'carrier_request' AND carrier_key = @key AND opened_at > @since`,
		pgx.StrictNamedArgs{"key": carrierKey, "since": since}).Scan(&n); err != nil {
		return 0, fmt.Errorf("count requests for the carrier: %w", err)
	}
	return n, nil
}

func (s pgTickets) Messaged(ctx context.Context, conversation, message uuid.UUID, from domain.From, at time.Time) (bool, error) {
	var reopened bool
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var t Ticket
		err := tx.QueryRow(ctx, `SELECT id, account_id, conversation_id, status, resolved_at FROM tickets WHERE conversation_id = @conversation FOR UPDATE`,
			pgx.StrictNamedArgs{"conversation": conversation}).Scan(&t.ID, &t.AccountID, &t.ConversationID, &t.Status, &t.ResolvedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock ticket: %w", err)
		}
		b := &pgx.Batch{}
		queueMessage(b, t, message, from, at, &reopened)
		if err = tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("record message: %w", err)
		}
		if !reopened {
			return nil
		}
		b = &pgx.Batch{}
		stream.Queue(b, []uuid.UUID{t.AccountID}, stream.Hint{Kind: stream.KindTicket, ConversationID: &t.ConversationID})
		if err = tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("notify: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("record a message: %w", err)
	}
	return reopened, nil
}

// recordMessage records message @message in ticket @id once. It moves the
// ticket's last message and, when @reopen asks and the owner @account lives,
// reopens the ticket with its event, whose ID is the message's; the share
// lock on the owner works as postgres.QueueLockLiving's. It selects whether
// it reopened.
const recordMessage = `
WITH receipt AS (
    INSERT INTO ticket_message_receipts (message_id, ticket_id) VALUES (@message, @id)
    ON CONFLICT (message_id) DO NOTHING
    RETURNING ticket_id),
living AS (
    SELECT 1 FROM accounts WHERE id = @account AND @reopen AND status <> 'deleted' FOR SHARE),
moved AS (
    UPDATE tickets SET last_message_at = greatest(last_message_at, @at),
        last_from = CASE WHEN @at >= last_message_at THEN @from ELSE last_from END,
        status = CASE WHEN r.reopen THEN 'open' ELSE status END,
        resolved_at = CASE WHEN r.reopen THEN NULL ELSE resolved_at END,
        resolved_by = CASE WHEN r.reopen THEN NULL ELSE resolved_by END
    FROM (SELECT EXISTS (SELECT 1 FROM living) AS reopen) r
    WHERE id IN (SELECT ticket_id FROM receipt)
    RETURNING r.reopen),
reopened AS (
    INSERT INTO ticket_events (id, ticket_id, kind, member_id, at)
    SELECT @message, @id, 'reopened', NULL, @at FROM moved WHERE reopen)
SELECT coalesce(bool_or(reopen), false) FROM moved`

// queueMessage queues recordMessage for message in t into b; reopened,
// when not nil, tells once b is sent whether the ticket reopened. Only the
// person writing after the ticket was resolved reopens it.
func queueMessage(b *pgx.Batch, t Ticket, message uuid.UUID, from domain.From, at time.Time, reopened *bool) {
	at = at.Truncate(time.Microsecond)
	reopen := from == domain.FromPerson && t.Status == domain.StatusResolved && t.ResolvedAt != nil && at.After(*t.ResolvedAt)
	b.Queue(recordMessage, pgx.StrictNamedArgs{
		"message": message, "id": t.ID, "account": t.AccountID, "reopen": reopen, "at": at, "from": from,
	}).QueryRow(func(row pgx.Row) error {
		var r bool
		if err := row.Scan(&r); err != nil {
			return fmt.Errorf("record message: %w", err)
		}
		if reopened != nil {
			*reopened = r
		}
		return nil
	})
}

// move sets a ticket's status to to, with its event (ID event), audit row
// and update, once the ticket's lock has shown the status differs.
func (s pgTickets) move(ctx context.Context, id, event uuid.UUID, to domain.Status, actor *audit.Actor, at time.Time) (Ticket, error) {
	var (
		member *uuid.UUID
		out    Ticket
	)
	if actor != nil && actor.Kind == audit.ActorMember {
		member = &actor.MemberID
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			status                domain.Status
			account, conversation uuid.UUID
		)
		err := tx.QueryRow(ctx, `SELECT status, account_id, conversation_id FROM tickets WHERE id = @id FOR UPDATE`,
			pgx.StrictNamedArgs{"id": id}).Scan(&status, &account, &conversation)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock ticket: %w", err)
		}
		if status == to {
			return domain.ErrNotAllowed
		}
		kind, action := "resolved", audit.TicketResolve
		resolvedAt, resolvedBy := &at, member
		if to == domain.StatusOpen {
			kind, action, resolvedAt, resolvedBy = "reopened", audit.TicketReopen, nil, nil
		}
		var moved []Ticket
		b := &pgx.Batch{}
		b.Queue(`UPDATE tickets SET status = @status, resolved_at = @resolved_at, resolved_by = @resolved_by
WHERE id = @id`, pgx.StrictNamedArgs{"id": id, "status": to, "resolved_at": resolvedAt, "resolved_by": resolvedBy})
		queueEvent(b, event, id, kind, member, at)
		if actor != nil {
			audit.Queue(b, audit.Entry{Actor: *actor, Action: action, Target: audit.Ticket(id), At: at})
		}
		queueRead(b, `WHERE t.id = @id`, pgx.StrictNamedArgs{"id": id}, &moved)
		stream.Queue(b, []uuid.UUID{account}, stream.Hint{Kind: stream.KindTicket, ConversationID: &conversation})
		if err = tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("update ticket: %w", err)
		}
		out = moved[0]
		return nil
	})
	if err != nil {
		return Ticket{}, fmt.Errorf("move ticket: %w", err)
	}
	return out, nil
}

func (s pgTickets) Resolve(ctx context.Context, id, event uuid.UUID, actor *audit.Actor, at time.Time) (Ticket, error) {
	return s.move(ctx, id, event, domain.StatusResolved, actor, at)
}

func (s pgTickets) Reopen(ctx context.Context, id, event uuid.UUID, actor *audit.Actor, at time.Time) (Ticket, error) {
	return s.move(ctx, id, event, domain.StatusOpen, actor, at)
}

func (s pgTickets) RecordReply(ctx context.Context, ticket, message uuid.UUID, actor *audit.Actor, at time.Time) error {
	var member *uuid.UUID
	if actor != nil && actor.Kind == audit.ActorMember {
		member = &actor.MemberID
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO ticket_replies (message_id, ticket_id, member_id) VALUES (@message, @ticket, @member)
ON CONFLICT (message_id) DO NOTHING`, pgx.StrictNamedArgs{"message": message, "ticket": ticket, "member": member})
		if err != nil {
			return fmt.Errorf("insert reply: %w", err)
		}
		if tag.RowsAffected() == 0 || actor == nil {
			return nil
		}
		b := &pgx.Batch{}
		audit.Queue(b, audit.Entry{
			Actor: *actor, Action: audit.TicketReply, Target: audit.Ticket(ticket), At: at,
			Details: map[string]any{"message_id": message},
		})
		if err = tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("audit: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record reply: %w", err)
	}
	return nil
}

func (s pgTickets) Members(ctx context.Context, messages []uuid.UUID) (map[uuid.UUID]Reply, error) {
	rows, err := s.db.Query(ctx, `SELECT r.message_id, coalesce(m.display_name, ''), r.member_id IS NULL
FROM ticket_replies r LEFT JOIN console_members m ON m.id = r.member_id WHERE r.message_id = ANY(@ids)`,
		pgx.StrictNamedArgs{"ids": messages})
	if err != nil {
		return nil, fmt.Errorf("select replies: %w", err)
	}
	out := map[uuid.UUID]Reply{}
	_, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct{}, error) {
		var (
			id uuid.UUID
			r  Reply
		)
		if scanErr := row.Scan(&id, &r.Member, &r.Automatic); scanErr != nil {
			return struct{}{}, fmt.Errorf("scan reply: %w", scanErr)
		}
		out[id] = r
		return struct{}{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read replies: %w", err)
	}
	return out, nil
}

// ResolveAllOf locks the account's open tickets, then resolves them with
// events derived from event and sends an update per conversation.
func (s pgTickets) ResolveAllOf(ctx context.Context, account, event uuid.UUID, at time.Time) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, conversation_id FROM tickets WHERE account_id = @account AND status = 'open'
ORDER BY id FOR UPDATE`, pgx.StrictNamedArgs{"account": account})
		if err != nil {
			return fmt.Errorf("lock open tickets: %w", err)
		}
		open, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct{ ID, Conversation uuid.UUID }])
		if err != nil {
			return fmt.Errorf("read open tickets: %w", err)
		}
		if len(open) == 0 {
			return nil
		}
		ids, events := make([]uuid.UUID, len(open)), make([]uuid.UUID, len(open))
		for i, t := range open {
			ids[i], events[i] = t.ID, restatex.DerivedID("ticket-resolved", event.String(), t.ID.String())
		}
		b := &pgx.Batch{}
		b.Queue(`UPDATE tickets SET status = 'resolved', resolved_at = @at, resolved_by = NULL WHERE id = ANY(@ids)`,
			pgx.StrictNamedArgs{"ids": ids, "at": at}).Fn = postgres.ExecAs("resolve tickets", nil)
		// A retry after a member reopened one finds its event there already.
		b.Queue(`INSERT INTO ticket_events (id, ticket_id, kind, member_id, at)
SELECT e, t, 'resolved', NULL, @at FROM unnest(@events::uuid[], @ids::uuid[]) AS u (e, t)
ON CONFLICT (id) DO NOTHING`, pgx.StrictNamedArgs{"events": events, "ids": ids, "at": at}).Fn = postgres.ExecAs("insert the resolved events", nil)
		for _, t := range open {
			stream.Queue(b, []uuid.UUID{account}, stream.Hint{Kind: stream.KindTicket, ConversationID: &t.Conversation})
		}
		if err = tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("resolve tickets: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("resolve the account's tickets: %w", err)
	}
	return nil
}
