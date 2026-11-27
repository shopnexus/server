package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const approvalColumns = `a.id, a.conversation_id, a.turn_id, a.member_id, a.operation, a.method, a.path, a.headers, a.body,
    a.status, a.decided_by, a.decided_at, a.result_status, a.result_body, a.created_at`

func scanApproval(r pgx.CollectableRow) (Approval, error) {
	var a Approval
	if err := r.Scan(&a.ID, &a.ConversationID, &a.TurnID, &a.MemberID, &a.Operation, &a.Method, &a.Path, &a.Headers,
		&a.Body, &a.Status, &a.DecidedBy, &a.DecidedAt, &a.ResultStatus, &a.ResultBody, &a.CreatedAt); err != nil {
		return a, fmt.Errorf("scan agent approval: %w", err)
	}
	return a, nil
}

func collectApprovals(rows pgx.Rows, err error) ([]Approval, error) {
	if err != nil {
		return nil, fmt.Errorf("select agent approvals: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanApproval)
	if err != nil {
		return nil, fmt.Errorf("read agent approvals: %w", err)
	}
	return out, nil
}

// jsonOrNil is raw for a jsonb column, NULL when empty.
func jsonOrNil(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func (s pgConversations) AddApproval(ctx context.Context, a Approval) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO agent_approvals (id, conversation_id, turn_id, member_id, operation, method,
    path, headers, body, status, created_at)
VALUES (@id, @conversation, @turn, @member, @operation, @method, @path, @headers, @body, 'pending', @at)`, pgx.StrictNamedArgs{
		"id": a.ID, "conversation": a.ConversationID, "turn": a.TurnID, "member": a.MemberID, "operation": a.Operation,
		"method": a.Method, "path": a.Path, "headers": a.Headers, "body": jsonOrNil(a.Body), "at": a.CreatedAt,
	}); err != nil {
		return fmt.Errorf("insert agent approval: %w", err)
	}
	return nil
}

func (s pgConversations) Approval(ctx context.Context, id uuid.UUID) (Approval, error) {
	list, err := collectApprovals(s.db.Query(ctx, `SELECT `+approvalColumns+` FROM agent_approvals AS a WHERE a.id = @id`,
		pgx.StrictNamedArgs{"id": id}))
	if err != nil {
		return Approval{}, err
	}
	if len(list) == 0 {
		return Approval{}, ErrNotFound
	}
	return list[0], nil
}

func (s pgConversations) Approvals(ctx context.Context, f ApprovalFilter) ([]Approval, error) {
	return collectApprovals(s.db.Query(ctx, `SELECT `+approvalColumns+` FROM agent_approvals AS a
JOIN agent_conversations AS c ON c.id = a.conversation_id
WHERE (c.subject_kind IS NOT NULL OR c.member_id = @member)
    AND (@conversation::uuid IS NULL OR a.conversation_id = @conversation)
    AND (@status = '' OR a.status = @status)
ORDER BY a.created_at DESC, a.id LIMIT @limit`, pgx.StrictNamedArgs{
		"member": f.Member, "conversation": f.ConversationID, "status": f.Status, "limit": f.Limit,
	}))
}

func (s pgConversations) Decide(ctx context.Context, id uuid.UUID, decide func(Approval) (Decision, error)) (Approval, error) {
	var out Approval
	err := inTx(ctx, s.db, "decide agent approval", func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+approvalColumns+` FROM agent_approvals AS a WHERE a.id = @id FOR UPDATE`,
			pgx.StrictNamedArgs{"id": id})
		if err != nil {
			return fmt.Errorf("lock agent approval: %w", err)
		}
		a, err := pgx.CollectExactlyOneRow(rows, scanApproval)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNotFound
		case err != nil:
			return fmt.Errorf("read agent approval: %w", err)
		case a.Status != "pending":
			return ErrState
		}
		d, err := decide(a)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE agent_approvals SET status = @status, decided_by = @by, decided_at = @at,
    result_status = @result_status, result_body = @result_body
WHERE id = @id`, pgx.StrictNamedArgs{
			"id": id, "status": d.Status, "by": d.By, "at": d.At, "result_status": d.ResultStatus,
			"result_body": jsonOrNil(d.ResultBody),
		}); err != nil {
			return fmt.Errorf("decide agent approval: %w", err)
		}
		a.Status, a.DecidedBy, a.DecidedAt, a.ResultStatus, a.ResultBody = d.Status, &d.By, &d.At, d.ResultStatus, d.ResultBody
		out = a
		return nil
	})
	return out, err
}

func (s pgConversations) Unreported(ctx context.Context, conversation uuid.UUID) ([]Approval, error) {
	return collectApprovals(s.db.Query(ctx, `SELECT `+approvalColumns+` FROM agent_approvals AS a
WHERE a.conversation_id = @id AND a.status <> 'pending' AND a.reported_at IS NULL ORDER BY a.decided_at, a.id`,
		pgx.StrictNamedArgs{"id": conversation}))
}

func (s pgConversations) MarkReported(ctx context.Context, ids []uuid.UUID, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := s.db.Exec(ctx, `UPDATE agent_approvals SET reported_at = @at WHERE id = ANY(@ids)`,
		pgx.StrictNamedArgs{"ids": ids, "at": at}); err != nil {
		return fmt.Errorf("mark agent approvals reported: %w", err)
	}
	return nil
}
