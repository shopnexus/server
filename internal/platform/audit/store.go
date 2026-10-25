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
	TargetID uuid.UUID `json:"-" query:"target_id"`
}

// Action is the Action schema in api/schemas/staff.yaml.
type Action struct {
	ID      uuid.UUID      `json:"id"`
	At      time.Time      `json:"at"`
	Actor   ActionActor    `json:"actor"`
	IP      string         `json:"ip,omitempty"`
	Action  ActionKind     `json:"action"`
	Target  ActionTarget   `json:"target"`
	Details map[string]any `json:"details"`
}

type ActionActor struct {
	Kind        ActorKind  `json:"kind"`
	StaffID     *uuid.UUID `json:"staff_id,omitempty"`
	DisplayName string     `json:"display_name"`
}

type ActionTarget struct {
	Kind TargetKind `json:"kind"`
	ID   uuid.UUID  `json:"id"`
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
SELECT a.id, a.at, a.actor_kind, a.staff_id, coalesce(m.display_name, a.actor_name), coalesce(host(a.ip), ''),
       a.action, a.target_kind, a.target_id, a.details
FROM staff_actions a
LEFT JOIN staff_members m ON m.id = a.staff_id
WHERE a.id < @before
  AND (@staff_id::uuid IS NULL OR a.staff_id = @staff_id)
  AND (@target_id::uuid IS NULL OR a.target_id = @target_id)
ORDER BY a.id DESC
LIMIT @limit`

func (s pgStore) List(ctx context.Context, f Filter, p page.Request) ([]Action, error) {
	rows, err := s.db.Query(ctx, listActions, pgx.StrictNamedArgs{
		"before": p.Before(), "limit": p.Fetch(), "staff_id": nullID(f.StaffID), "target_id": nullID(f.TargetID),
	})
	if err != nil {
		return nil, fmt.Errorf("select staff actions: %w", err)
	}
	defer rows.Close()
	var out []Action
	for rows.Next() {
		var a Action
		if err := rows.Scan(&a.ID, &a.At, &a.Actor.Kind, &a.Actor.StaffID, &a.Actor.DisplayName, &a.IP,
			&a.Action, &a.Target.Kind, &a.Target.ID, &a.Details); err != nil {
			return nil, fmt.Errorf("read staff action: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read staff actions: %w", err)
	}
	return out, nil
}
