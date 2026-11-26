package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// Parent tables no row is ever deleted from: a key into them needs no index.
var neverDeleted = []string{
	"communes", "provinces", "categories", "specs",
	"console_members", "console_sessions", "ai_agent_versions", "ai_tags",
}

// Every foreign key into a table rows are deleted from has an index that
// leads with its columns, so deleting a parent does not scan the child. A
// partial index counts only when its condition is the key being set.
func TestForeignKeysHaveIndexes(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	rows, err := db.Query(context.Background(), `
SELECT c.conrelid::regclass::text || ' (' || array_to_string(cols.names, ', ') || ')'
FROM pg_constraint AS c
JOIN pg_namespace AS n ON n.oid = c.connamespace
CROSS JOIN LATERAL (
    SELECT array_agg(a.attname ORDER BY k.n) AS names
    FROM unnest(c.conkey) WITH ORDINALITY AS k (attnum, n)
    JOIN pg_attribute AS a ON a.attrelid = c.conrelid AND a.attnum = k.attnum) AS cols
WHERE c.contype = 'f' AND n.nspname = 'public'
  AND c.confrelid::regclass::text <> ALL(@skip)
  AND NOT EXISTS (
    SELECT 1 FROM pg_index AS i
    WHERE i.indrelid = c.conrelid
      AND (i.indkey::int2[])[0:cardinality(c.conkey) - 1] @> c.conkey
      AND (i.indkey::int2[])[0:cardinality(c.conkey) - 1] <@ c.conkey
      AND (i.indpred IS NULL
        OR (cardinality(cols.names) = 1
          AND pg_get_expr(i.indpred, i.indrelid) = '(' || cols.names[1] || ' IS NOT NULL)')))
ORDER BY 1`, pgx.StrictNamedArgs{"skip": neverDeleted})
	if err != nil {
		t.Fatal(err)
	}
	missing, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range missing {
		t.Errorf("foreign key without an index: %s", m)
	}
}
