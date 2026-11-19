package loader

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/shopnexus/server/internal/seedpack"
)

// WipePlan is what Wipe deletes: the pack's people who are in the stack,
// and with them everything that refers to them.
type WipePlan struct {
	Pack     seedpack.Manifest `json:"pack"`
	People   int               `json:"people"`
	Listings int               `json:"listings"`
	Uploads  int               `json:"uploads"`
	// Others counts rows of the stack's other people that go too: their
	// deals, messages and follows with the pack's people.
	Deals    int `json:"deals"`
	Messages int `json:"messages"`

	ids []uuid.UUID
}

// PlanWipe finds the pack's people in the stack by their IDs.
func (l *Loader) PlanWipe(ctx context.Context, p *seedpack.Pack) (*WipePlan, error) {
	accounts, err := p.Accounts()
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(accounts))
	for i := range accounts {
		a := &accounts[i]
		ids[i] = p.Manifest.AccountID(a)
	}
	there, err := existing(ctx, l.d.Pool, "accounts", ids)
	if err != nil {
		return nil, err
	}
	w := &WipePlan{Pack: p.Manifest}
	for _, id := range ids {
		if there[id] {
			w.ids = append(w.ids, id)
		}
	}
	w.People = len(w.ids)
	err = l.d.Pool.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM listings WHERE seller_id = ANY(@ids)),
    (SELECT count(*) FROM uploads WHERE owner_id = ANY(@ids)),
    (SELECT count(*) FROM deals WHERE buyer_id = ANY(@ids) OR seller_id = ANY(@ids)),
    (SELECT count(*) FROM messages m JOIN conversations c ON c.id = m.conversation_id
     WHERE c.account_a = ANY(@ids) OR c.account_b = ANY(@ids))`,
		pgx.StrictNamedArgs{"ids": w.ids}).Scan(&w.Listings, &w.Uploads, &w.Deals, &w.Messages)
	if err != nil {
		return nil, fmt.Errorf("count the pack's rows: %w", err)
	}
	return w, nil
}

// WipeResult is what Wipe deleted.
type WipeResult struct {
	Rows    int64 `json:"rows"`
	Objects int   `json:"objects"`
}

// Wipe deletes the plan's people and every row that refers to them, in one
// transaction, then their uploads' objects. It follows the foreign keys of
// the stack's schema, so a table added later goes too.
func (l *Loader) Wipe(ctx context.Context, w *WipePlan, prog seedpack.Progress) (WipeResult, error) {
	var res WipeResult
	if len(w.ids) == 0 {
		return res, nil
	}
	// Every object of the people's uploads: originals, photos, thumbnails.
	rows, err := l.d.Pool.Query(ctx, `SELECT id, coalesce(photo_key, ''), coalesce(thumb_key, '') FROM uploads WHERE owner_id = ANY(@ids)`,
		pgx.StrictNamedArgs{"ids": w.ids})
	if err != nil {
		return res, fmt.Errorf("read the uploads: %w", err)
	}
	var keys []string
	for rows.Next() {
		var (
			id           uuid.UUID
			photo, thumb string
		)
		if err = rows.Scan(&id, &photo, &thumb); err != nil {
			rows.Close()
			return res, fmt.Errorf("read the uploads: %w", err)
		}
		keys = append(keys, "uploads/"+id.String())
		for _, k := range []string{photo, thumb} {
			if k != "" {
				keys = append(keys, k)
			}
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return res, fmt.Errorf("read the uploads: %w", err)
	}

	fks, err := foreignKeys(ctx, l)
	if err != nil {
		return res, err
	}
	bar := prog.Stage("rows", 0)
	err = pgx.BeginFunc(ctx, l.d.Pool, func(tx pgx.Tx) error {
		n, delErr := deleteFrom(ctx, tx, fks, "accounts", "id = ANY(@ids)", w.ids, nil)
		res.Rows = n
		bar.Add(n)
		return delErr
	})
	bar.Done()
	if err != nil {
		return res, fmt.Errorf("delete the pack's rows: %w", err)
	}

	obar := prog.Stage("objects", int64(len(keys)))
	defer obar.Done()
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for _, k := range keys {
		g.Go(func() error {
			defer obar.Add(1)
			// A key the store lacks (an original already processed) is fine.
			if delErr := l.d.Store.Delete(gctx, k); delErr != nil {
				return fmt.Errorf("delete %s: %w", k, delErr)
			}
			return nil
		})
	}
	if err = g.Wait(); err != nil {
		return res, fmt.Errorf("delete the objects: %w", err)
	}
	res.Objects = len(keys)
	return res, nil
}

// foreignKey is a one-column foreign key that blocks a delete: NO ACTION
// or RESTRICT. CASCADE and SET NULL keys the database handles.
type foreignKey struct {
	Child, ChildCol, Parent, ParentCol string
}

func foreignKeys(ctx context.Context, l *Loader) ([]foreignKey, error) {
	rows, err := l.d.Pool.Query(ctx, `
SELECT c.conrelid::regclass::text, a.attname::text, c.confrelid::regclass::text, pa.attname::text
FROM pg_constraint c
JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
JOIN pg_attribute pa ON pa.attrelid = c.confrelid AND pa.attnum = c.confkey[1]
WHERE c.contype = 'f' AND cardinality(c.conkey) = 1 AND c.confdeltype IN ('a', 'r')
ORDER BY 1, 2`)
	if err != nil {
		return nil, fmt.Errorf("read the foreign keys: %w", err)
	}
	fks, err := pgx.CollectRows(rows, pgx.RowToStructByPos[foreignKey])
	if err != nil {
		return nil, fmt.Errorf("read the foreign keys: %w", err)
	}
	return fks, nil
}

// deleteFrom deletes table's rows that where selects after the rows that
// refer to them, depth first; a table already on the path is skipped.
func deleteFrom(ctx context.Context, tx pgx.Tx, fks []foreignKey, table, where string, ids []uuid.UUID, path []string) (int64, error) {
	path = append(slices.Clip(path), table)
	var n int64
	for _, fk := range fks {
		if fk.Parent != table || slices.Contains(path, fk.Child) {
			continue
		}
		sub := fmt.Sprintf("%s IN (SELECT %s FROM %s WHERE %s)", ident(fk.ChildCol), ident(fk.ParentCol), ident(table), where)
		m, err := deleteFrom(ctx, tx, fks, fk.Child, sub, ids, path)
		n += m
		if err != nil {
			return n, err
		}
	}
	tag, err := tx.Exec(ctx, "DELETE FROM "+ident(table)+" WHERE "+where, pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return n, fmt.Errorf("delete from %s: %w", table, err)
	}
	return n + tag.RowsAffected(), nil
}

func ident(name string) string {
	parts := strings.Split(name, ".")
	return pgx.Identifier(parts).Sanitize()
}
