package postgres

import (
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Counts reads rows of a key and a count, such as those of
// `SELECT status, count(*) ... GROUP BY 1`, into a map. A key the query
// returns no row for is left out.
func Counts[K ~string](rows pgx.Rows, err error) (map[K]int, error) {
	if err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}
	counts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct {
		Key K
		N   int
	}])
	if err != nil {
		return nil, fmt.Errorf("read counts: %w", err)
	}
	out := make(map[K]int, len(counts))
	for _, c := range counts {
		out[c.Key] = c.N
	}
	return out, nil
}
