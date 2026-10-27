package repository

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

type pgSearches struct{ db *pgxpool.Pool }

func NewSearches(db *pgxpool.Pool) Searches { return pgSearches{db} }

// query assembles one ParadeDB query from fixed fragments and generated
// parameter names; no caller value reaches the SQL text. Every parameter
// carries its type, which ParadeDB's overloaded builders need.
type query struct {
	args pgx.StrictNamedArgs
	n    int
}

func (q *query) arg(v any, cast string) string {
	name := fmt.Sprintf("p%d", q.n)
	q.n++
	q.args[name] = v
	return "@" + name + "::" + cast
}

func array(items []string) string {
	return "ARRAY[" + strings.Join(items, ", ") + "]::paradedb.searchqueryinput[]"
}

// kmPerDegree is the length of a degree of latitude.
const kmPerDegree = 111.32

// box is the lat and lng ranges around p that hold a circle of radius km.
func (q *query) box(p domain.Point, km float64) string {
	dLat := km / kmPerDegree
	dLng := km / (kmPerDegree * math.Cos(p.Lat*math.Pi/180))
	return `paradedb.boolean(must => ` + array([]string{
		`paradedb.range('lat', numrange(` + q.arg(p.Lat-dLat, "numeric") + `, ` + q.arg(p.Lat+dLat, "numeric") + `, '[]'))`,
		`paradedb.range('lng', numrange(` + q.arg(p.Lng-dLng, "numeric") + `, ` + q.arg(p.Lng+dLng, "numeric") + `, '[]'))`,
	}) + `)`
}

// priceRange matches prices between lo and hi; 0 leaves that end open.
func (q *query) priceRange(lo, hi int64) string {
	var low, high any
	if lo > 0 {
		low = lo
	}
	if hi > 0 {
		high = hi
	}
	return `paradedb.range('price', int8range(` + q.arg(low, "bigint") + `, ` + q.arg(high, "bigint") + `, '[]'))`
}

// must is the filters, and with phrases the rule that one of them matches.
func (q *query) must(s domain.Search) []string {
	f := s.Filters
	out := []string{`paradedb.term('status', ` + q.arg(string(domain.StatusActive), "text") + `)`}
	if f.CategoryRoot != "" {
		out = append(out, `paradedb.term('category_root', `+q.arg(f.CategoryRoot, "text")+`)`)
	}
	if f.CategoryCode != "" {
		out = append(out, `paradedb.term('category_code', `+q.arg(f.CategoryCode, "text")+`)`)
	}
	if f.PriceMin > 0 || f.PriceMax > 0 {
		out = append(out, q.priceRange(f.PriceMin, f.PriceMax))
	}
	if len(f.Conditions) > 0 {
		conds := make([]string, 0, len(f.Conditions))
		for _, c := range f.Conditions {
			conds = append(conds, `paradedb.term('condition', `+q.arg(string(c), "text")+`)`)
		}
		out = append(out, `paradedb.boolean(should => `+array(conds)+`)`)
	}
	if f.OffersOnly {
		out = append(out, `paradedb.term('allow_offers', true)`)
	}
	if f.Origin != nil && f.RadiusKm > 0 {
		out = append(out, q.box(*f.Origin, float64(f.RadiusKm)))
	}
	if len(s.Phrases) > 0 {
		var phrases []string
		for _, p := range s.Phrases {
			if p.Weight > 0 {
				phrases = append(phrases, q.phrase(p)...)
			}
		}
		if len(phrases) == 0 {
			phrases = []string{`paradedb.empty()`}
		}
		out = append(out, `paradedb.boolean(should => `+array(phrases)+`)`)
	}
	return out
}

// fuzzyRunes is the length from which a word may match with one edit.
const fuzzyRunes = 5

// phrase matches the title at twice its weight, description and specs at
// its weight, and the title's long words with one edit at half of it.
func (q *query) phrase(p domain.Phrase) []string {
	text := q.arg(p.Text, "text")
	out := []string{
		`paradedb.boost(` + q.arg(2*p.Weight, "real") + `, paradedb.match('title', ` + text + `))`,
		`paradedb.boost(` + q.arg(p.Weight, "real") + `, paradedb.match('description', ` + text + `))`,
		`paradedb.boost(` + q.arg(p.Weight, "real") + `, paradedb.match('specs_text', ` + text + `))`,
	}
	var long []string
	for w := range strings.FieldsSeq(p.Text) {
		if utf8.RuneCountInString(w) >= fuzzyRunes {
			long = append(long, w)
		}
	}
	if len(long) > 0 {
		out = append(out, `paradedb.boost(`+q.arg(p.Weight/2, "real")+`, paradedb.match('title', `+
			q.arg(strings.Join(long, " "), "text")+`, distance => 1))`)
	}
	return out
}

// should is the boosts as constant scores, and the demoted phrases.
func (q *query) should(s domain.Search) []string {
	var out []string
	for _, p := range s.Phrases {
		if p.Weight < 0 {
			out = append(out, q.phrase(p)...)
		}
	}
	for _, b := range s.Boosts {
		var clause string
		switch b.Kind {
		case domain.BoostCategory:
			clause = `paradedb.term('category_code', ` + q.arg(b.Code, "text") + `)`
		case domain.BoostCategoryRoot:
			clause = `paradedb.term('category_root', ` + q.arg(b.Code, "text") + `)`
		case domain.BoostPrice:
			clause = q.priceRange(b.PriceMin, b.PriceMax)
		case domain.BoostCondition:
			clause = `paradedb.term('condition', ` + q.arg(string(b.Condition), "text") + `)`
		case domain.BoostOffers:
			clause = `paradedb.term('allow_offers', true)`
		case domain.BoostNear:
			clause = q.box(b.Near, b.RadiusKm)
		case domain.BoostFresh:
			clause = `paradedb.range('published_at', tstzrange(` + q.arg(b.Since, "timestamptz") + `, NULL, '[]'))`
		default:
			continue // a kind nothing here knows cannot reach the query
		}
		out = append(out, `paradedb.const_score(`+q.arg(b.Weight, "real")+`, `+clause+`)`)
	}
	return out
}

// where is the search's condition; counts leave the boosts out, since they
// change no membership.
func (q *query) where(s domain.Search, boosts bool) string {
	sql := `id @@@ paradedb.boolean(must => ` + array(q.must(s))
	if !boosts {
		// Building the boosts would add their parameters, which strict
		// named args refuse when the SQL does not use them.
		return sql + `)`
	}
	if should := q.should(s); len(should) > 0 {
		sql += `, should => ` + array(should)
	}
	return sql + `)`
}

// distanceSQL is the great-circle distance in km from the origin parameters.
const distanceSQL = `(12742 * asin(sqrt(power(sin(radians(lat - @olat) / 2), 2)
       + cos(radians(@olat)) * cos(radians(lat)) * power(sin(radians(lng - @olng) / 2), 2))))`

func (s pgSearches) Search(ctx context.Context, in domain.Search) ([]domain.Hit, error) {
	q := query{args: pgx.StrictNamedArgs{"limit": in.Limit, "offset": in.Offset}}
	sql := `SELECT id, lat, lng FROM listings WHERE ` + q.where(in, true)
	if o := in.Filters.Origin; o != nil && (in.Filters.RadiusKm > 0 || in.Sort == domain.SortNearest) {
		q.args["olat"], q.args["olng"] = o.Lat, o.Lng
		if in.Filters.RadiusKm > 0 {
			q.args["radius"] = in.Filters.RadiusKm
			sql += ` AND ` + distanceSQL + ` <= @radius`
		}
	}
	switch in.Sort {
	case domain.SortNewest:
		sql += ` ORDER BY published_at DESC, id DESC`
	case domain.SortPriceAsc:
		sql += ` ORDER BY price ASC, id`
	case domain.SortPriceDesc:
		sql += ` ORDER BY price DESC, id`
	case domain.SortNearest:
		sql += ` ORDER BY ` + distanceSQL + `, id`
	case domain.SortRelevance:
		sql += ` ORDER BY paradedb.score(id) DESC, id`
	default:
		sql += ` ORDER BY paradedb.score(id) DESC, id`
	}
	rows, err := s.db.Query(ctx, sql+` LIMIT @limit OFFSET @offset`, q.args)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()
	var out []domain.Hit
	for rows.Next() {
		var h domain.Hit
		if err = rows.Scan(&h.ID, &h.Point.Lat, &h.Point.Lng); err != nil {
			return nil, fmt.Errorf("scan hit: %w", err)
		}
		out = append(out, h)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("search rows: %w", err)
	}
	return out, nil
}

func (s pgSearches) Counts(ctx context.Context, in domain.Search, by domain.CountField) ([]domain.CategoryCount, error) {
	q := query{args: pgx.StrictNamedArgs{}}
	column := "category_code"
	if by == domain.CountRoot {
		column = "category_root"
	}
	rows, err := s.db.Query(ctx, `SELECT `+column+`, count(*) FROM listings WHERE `+q.where(in, false)+
		` GROUP BY 1 ORDER BY 2 DESC, 1`, q.args)
	if err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}
	defer rows.Close()
	var out []domain.CategoryCount
	for rows.Next() {
		var c domain.CategoryCount
		if err = rows.Scan(&c.Code, &c.Count); err != nil {
			return nil, fmt.Errorf("scan count: %w", err)
		}
		out = append(out, c)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("count rows: %w", err)
	}
	return out, nil
}
