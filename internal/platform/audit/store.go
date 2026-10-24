package audit

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/page"
)

// Filter narrows a listing; a zero field filters nothing. A handler binds it
// from the query, embedded next to page.Request.
type Filter struct {
	StaffID  uuid.UUID `json:"-" query:"staff_id"`
	Action   string    `json:"-" query:"action"`
	TargetID uuid.UUID `json:"-" query:"target_id"`
	Since    time.Time `json:"-" query:"since"`
	Until    time.Time `json:"-" query:"until"`
}

// Action is the Action schema in api/schemas/staff.yaml.
type Action struct {
	ID     uuid.UUID      `json:"id"`
	At     time.Time      `json:"at"`
	Actor  ActionActor    `json:"actor"`
	IP     string         `json:"ip,omitempty"`
	Action string         `json:"action"`
	Target *ActionTarget  `json:"target,omitempty"`
	Diff   map[string]any `json:"diff"`
}

type ActionActor struct {
	Kind        ActorKind  `json:"kind"`
	StaffID     *uuid.UUID `json:"staff_id,omitempty"`
	DisplayName string     `json:"display_name,omitempty"`
}

type ActionTarget struct {
	Kind string    `json:"kind"`
	ID   uuid.UUID `json:"id"`
}

// Store reads the audit log.
type Store interface {
	// List returns the actions f matches with IDs below p.Before(), newest
	// first, at most p.Fetch().
	List(ctx context.Context, f Filter, p page.Request) ([]Action, error)
}

type pgStore struct{ db *pgxpool.Pool }

func NewPGStore(db *pgxpool.Pool) Store { return pgStore{db} }

const listActions = `
SELECT a.id, a.at, a.actor_kind, a.staff_id, coalesce(m.display_name, a.actor_name, ''), coalesce(host(a.ip), ''),
       a.action, coalesce(a.target_kind, ''), a.target_id, a.diff
FROM staff_actions a
LEFT JOIN staff_members m ON m.id = a.staff_id
WHERE a.id < @before
  AND (@staff_id::uuid IS NULL OR a.staff_id = @staff_id)
  AND (@action::text IS NULL OR a.action = @action)
  AND (@target_id::uuid IS NULL OR a.target_id = @target_id)
  AND (@since::timestamptz IS NULL OR a.at >= @since)
  AND (@until::timestamptz IS NULL OR a.at < @until)
ORDER BY a.id DESC
LIMIT @limit`

func (s pgStore) List(ctx context.Context, f Filter, p page.Request) ([]Action, error) {
	rows, err := s.db.Query(ctx, listActions, pgx.StrictNamedArgs{
		"before": p.Before(), "limit": p.Fetch(), "staff_id": nullID(f.StaffID), "action": nullText(f.Action),
		"target_id": nullID(f.TargetID), "since": nullTime(f.Since), "until": nullTime(f.Until),
	})
	if err != nil {
		return nil, fmt.Errorf("select staff actions: %w", err)
	}
	defer rows.Close()
	var out []Action
	for rows.Next() {
		var (
			a          Action
			targetKind string
			targetID   *uuid.UUID
		)
		if err := rows.Scan(&a.ID, &a.At, &a.Actor.Kind, &a.Actor.StaffID, &a.Actor.DisplayName, &a.IP,
			&a.Action, &targetKind, &targetID, &a.Diff); err != nil {
			return nil, fmt.Errorf("read staff action: %w", err)
		}
		if targetID != nil {
			a.Target = &ActionTarget{Kind: targetKind, ID: *targetID}
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read staff actions: %w", err)
	}
	return out, nil
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
