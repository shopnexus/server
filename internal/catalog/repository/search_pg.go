package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/pgvec"
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

// box matches the points in domain.Box around p.
func (q *query) box(p domain.Point, km float64) string {
	lo, hi := domain.Box(p, km)
	return `paradedb.boolean(must => ` + array([]string{
		`paradedb.range('lat', numrange(` + q.arg(lo.Lat, "numeric") + `, ` + q.arg(hi.Lat, "numeric") + `, '[]'))`,
		`paradedb.range('lng', numrange(` + q.arg(lo.Lng, "numeric") + `, ` + q.arg(hi.Lng, "numeric") + `, '[]'))`,
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
			switch {
			case p.Weight > 0 && p.AllWords:
				phrases = append(phrases, q.allWords(p))
			case p.Weight > 0:
				phrases = append(phrases, q.phrase(p)...)
			default:
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

// allWords matches a listing holding every word of p, each in its title,
// description or specs. A word is a run of letters and digits; a phrase of
// none matches nothing.
func (q *query) allWords(p domain.Phrase) string {
	var words []string
	for _, w := range strings.FieldsFunc(p.Text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		text := q.arg(w, "text")
		words = append(words, `paradedb.boolean(should => `+array([]string{
			`paradedb.match('title', `+text+`)`,
			`paradedb.match('description', `+text+`)`,
			`paradedb.match('specs_text', `+text+`)`,
		})+`)`)
	}
	if len(words) == 0 {
		return `paradedb.empty()`
	}
	return `paradedb.boolean(must => ` + array(words) + `)`
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

// where is the search's condition with its exclusions; counts leave the
// boosts out, since they change no membership.
func (q *query) where(s domain.Search, boosts bool) string {
	sql := `id @@@ paradedb.boolean(must => ` + array(q.must(s))
	// Building the boosts adds their parameters, which strict named args
	// refuse when the SQL does not use them, so counts never build them.
	if boosts {
		if should := q.should(s); len(should) > 0 {
			sql += `, should => ` + array(should)
		}
	}
	sql += `)`
	if ex := q.exclusions(s, ""); len(ex) > 0 {
		sql += ` AND ` + strings.Join(ex, " AND ")
	}
	return sql
}

// distanceSQL is the great-circle distance in km from the origin parameters.
const distanceSQL = `(12742 * asin(sqrt(power(sin(radians(lat - @olat) / 2), 2)
       + cos(radians(@olat)) * cos(radians(lat)) * power(sin(radians(lng - @olng) / 2), 2))))`

// photoScan is how many photo rows the photo list reads before it groups
// them by listing: about four photos a listing.
const photoScan = 4 * domain.MaxResults

// filters is the person's filters and the exclusions as plain SQL on the
// listings alias l, for the vector lists and the boost list. The BM25 list
// gets the same filters through must.
func (q *query) filters(s domain.Search) string {
	f := s.Filters
	out := []string{`l.status = ` + q.arg(string(domain.StatusActive), "text")}
	if f.CategoryRoot != "" {
		out = append(out, `l.category_root = `+q.arg(f.CategoryRoot, "text"))
	}
	if f.CategoryCode != "" {
		out = append(out, `l.category_code = `+q.arg(f.CategoryCode, "text"))
	}
	if f.PriceMin > 0 {
		out = append(out, `l.price >= `+q.arg(f.PriceMin, "bigint"))
	}
	if f.PriceMax > 0 {
		out = append(out, `l.price <= `+q.arg(f.PriceMax, "bigint"))
	}
	if len(f.Conditions) > 0 {
		conds := make([]string, len(f.Conditions))
		for i, c := range f.Conditions {
			conds[i] = string(c)
		}
		out = append(out, `l.condition = ANY(`+q.arg(conds, "text[]")+`)`)
	}
	if f.OffersOnly {
		out = append(out, `l.allow_offers`)
	}
	if f.Origin != nil && f.RadiusKm > 0 {
		out = append(out, distanceSQL+` <= @radius`)
	}
	return strings.Join(append(out, q.exclusions(s, "l.")...), " AND ")
}

// exclusions are the plain SQL conditions on the listings alias: the
// similar-listings source, the IDs filter and the listings and sellers left
// out.
func (q *query) exclusions(s domain.Search, alias string) []string {
	var out []string
	if s.Like != (uuid.UUID{}) {
		out = append(out, alias+`id <> `+q.arg(s.Like, "uuid"))
	}
	if len(s.Filters.IDs) > 0 {
		out = append(out, alias+`id = ANY(`+q.arg(s.Filters.IDs, "uuid[]")+`)`)
	}
	if len(s.ExcludeListings) > 0 {
		out = append(out, alias+`id <> ALL(`+q.arg(s.ExcludeListings, "uuid[]")+`)`)
	}
	if len(s.ExcludeSellers) > 0 {
		out = append(out, alias+`seller_id <> ALL(`+q.arg(s.ExcludeSellers, "uuid[]")+`)`)
	}
	return out
}

// points is a listing's boost points as plain SQL on l: each boost's
// weight when it matches.
func (q *query) points(boosts []domain.Boost) string {
	terms := make([]string, 1, 1+len(boosts))
	terms[0] = "0"
	for _, b := range boosts {
		var cond string
		switch b.Kind {
		case domain.BoostCategory:
			cond = `l.category_code = ` + q.arg(b.Code, "text")
		case domain.BoostCategoryRoot:
			cond = `l.category_root = ` + q.arg(b.Code, "text")
		case domain.BoostPrice:
			var c []string
			if b.PriceMin > 0 {
				c = append(c, `l.price >= `+q.arg(b.PriceMin, "bigint"))
			}
			if b.PriceMax > 0 {
				c = append(c, `l.price <= `+q.arg(b.PriceMax, "bigint"))
			}
			if len(c) == 0 {
				continue
			}
			cond = strings.Join(c, " AND ")
		case domain.BoostCondition:
			cond = `l.condition = ` + q.arg(string(b.Condition), "text")
		case domain.BoostOffers:
			cond = `l.allow_offers`
		case domain.BoostNear:
			lo, hi := domain.Box(b.Near, b.RadiusKm)
			cond = `l.lat BETWEEN ` + q.arg(lo.Lat, "float8") + ` AND ` + q.arg(hi.Lat, "float8") +
				` AND l.lng BETWEEN ` + q.arg(lo.Lng, "float8") + ` AND ` + q.arg(hi.Lng, "float8")
		case domain.BoostFresh:
			cond = `l.published_at >= ` + q.arg(b.Since, "timestamptz")
		default:
			continue
		}
		terms = append(terms, `CASE WHEN `+cond+` THEN `+q.arg(b.Weight, "float8")+` ELSE 0 END`)
	}
	return strings.Join(terms, " + ")
}

// hybrid is the WITH clause of a hybrid search: one CTE per list, each at
// most MaxResults listings ranked from 1, and fused, the weighted RRF score
// of every listing a list found.
func (q *query) hybrid(s domain.Search) string {
	// Strict named args refuse a parameter the SQL does not use: the origin
	// goes in only with a radius or the nearest sort, the list size only
	// with a list.
	if o := s.Filters.Origin; o != nil && (s.Filters.RadiusKm > 0 || s.Sort == domain.SortNearest) {
		q.args["olat"], q.args["olng"] = o.Lat, o.Lng
		if s.Filters.RadiusKm > 0 {
			q.args["radius"] = s.Filters.RadiusKm
		}
	}
	var ctes, lists []string
	share := func(name string, w float64) {
		q.args["list"] = domain.MaxResults
		lists = append(lists, name)
		ctes = append(ctes, name+`_share AS (SELECT id, `+q.arg(w, "float8")+` / (`+
			strconv.Itoa(domain.RRFK)+` + rank) AS share FROM `+name+`)`)
	}
	if s.Weights.BM25 > 0 && len(s.Phrases) > 0 {
		where := q.where(s, true)
		if s.Filters.Origin != nil && s.Filters.RadiusKm > 0 {
			where += ` AND ` + distanceSQL + ` <= @radius`
		}
		ctes = append(ctes, `bm25 AS (SELECT id, row_number() OVER (ORDER BY score DESC, id) AS rank FROM (
SELECT id, paradedb.score(id) AS score FROM listings WHERE `+where+` ORDER BY score DESC, id LIMIT @list) b)`)
		share("bm25", s.Weights.BM25)
	}
	if s.Weights.Text > 0 && s.Vectors.Text != nil {
		q.args["tq"], q.args["tv"], q.args["tmax"] = pgvec.Format(s.Vectors.Text), s.Vectors.TextVersion, 1-s.Vectors.TextMin
		ctes = append(ctes, `txt AS (SELECT id, row_number() OVER (ORDER BY d, id) AS rank FROM (
SELECT t.listing_id AS id, t.embedding <=> @tq::halfvec(1024) AS d
FROM listing_text_vectors AS t JOIN listings AS l ON l.id = t.listing_id
WHERE t.model_version = @tv AND `+q.filters(s)+` AND t.embedding <=> @tq::halfvec(1024) <= @tmax
ORDER BY t.embedding <=> @tq::halfvec(1024) LIMIT @list) v)`)
		share("txt", s.Weights.Text)
	}
	if s.Weights.Photo > 0 && s.Vectors.Photo != nil {
		q.args["pq"], q.args["pv"], q.args["pmax"], q.args["scan"] =
			pgvec.Format(s.Vectors.Photo), s.Vectors.PhotoVersion, 1-s.Vectors.PhotoMin, photoScan
		ctes = append(ctes, `img AS (SELECT id, row_number() OVER (ORDER BY d, id) AS rank FROM (
SELECT id, min(d) AS d FROM (
    SELECT p.listing_id AS id, p.embedding <=> @pq::halfvec(1152) AS d
    FROM listing_photo_vectors AS p JOIN listings AS l ON l.id = p.listing_id
    WHERE p.model_version = @pv AND `+q.filters(s)+` AND p.embedding <=> @pq::halfvec(1152) <= @pmax
    ORDER BY p.embedding <=> @pq::halfvec(1152) LIMIT @scan) x
GROUP BY id ORDER BY d, id LIMIT @list) v)`)
		share("img", s.Weights.Photo)
	}
	if s.Weights.Boost > 0 && len(s.Boosts) > 0 && len(lists) > 0 {
		pool := make([]string, len(lists))
		for i, l := range lists {
			pool[i] = `SELECT id FROM ` + l
		}
		ctes = append(ctes, `boost AS (SELECT id, row_number() OVER (ORDER BY points DESC, id) AS rank FROM (
SELECT l.id, `+q.points(s.Boosts)+` AS points FROM listings AS l JOIN (`+strings.Join(pool, " UNION ")+`) AS c ON c.id = l.id) p
WHERE points > 0)`)
		share("boost", s.Weights.Boost)
	}
	if len(lists) == 0 {
		// Nothing to fuse, such as a photo search whose list was left out:
		// an empty page rather than every listing.
		ctes = append(ctes, `fused AS (SELECT NULL::uuid AS id, 0::float8 AS score WHERE false)`)
		return `WITH ` + strings.Join(ctes, ",\n")
	}
	shares := make([]string, len(lists))
	for i, l := range lists {
		shares[i] = `SELECT id, share FROM ` + l + `_share`
	}
	ctes = append(ctes, `fused AS (SELECT id, sum(share) AS score FROM (`+strings.Join(shares, " UNION ALL ")+`) s GROUP BY id)`)
	return `WITH ` + strings.Join(ctes, ",\n")
}

func (s pgSearches) Search(ctx context.Context, in domain.Search) ([]domain.Hit, error) {
	if in.Hybrid() {
		return s.hybridSearch(ctx, in)
	}
	q := query{args: pgx.StrictNamedArgs{"limit": in.Limit, "offset": in.Offset}}
	sql := `SELECT id, lat, lng FROM listings WHERE ` + q.where(in, true)
	if o := in.Filters.Origin; o != nil && (in.Filters.RadiusKm > 0 || in.Sort == domain.SortNearest) {
		q.args["olat"], q.args["olng"] = o.Lat, o.Lng
		if in.Filters.RadiusKm > 0 {
			q.args["radius"] = in.Filters.RadiusKm
			sql += ` AND ` + distanceSQL + ` <= @radius`
		}
	}
	switch in.Sort { //nolint:exhaustive // relevance is the default order
	case domain.SortNewest:
		sql += ` ORDER BY published_at DESC, id DESC`
	case domain.SortPriceAsc:
		sql += ` ORDER BY price ASC, id`
	case domain.SortPriceDesc:
		sql += ` ORDER BY price DESC, id`
	case domain.SortNearest:
		sql += ` ORDER BY ` + distanceSQL + `, id`
	default:
		sql += ` ORDER BY paradedb.score(id) DESC, id`
	}
	rows, err := s.db.Query(ctx, sql+` LIMIT @limit OFFSET @offset`, q.args)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	hits, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Hit, error) {
		var h domain.Hit
		return h, row.Scan(&h.ID, &h.Point.Lat, &h.Point.Lng)
	})
	if err != nil {
		return nil, fmt.Errorf("read hits: %w", err)
	}
	return hits, nil
}

func (s pgSearches) Counts(ctx context.Context, in domain.Search, by domain.CountField) ([]domain.CategoryCount, error) {
	if in.Hybrid() {
		q := query{args: pgx.StrictNamedArgs{}}
		column := "l.category_code"
		if by == domain.CountRoot {
			column = "l.category_root"
		}
		var counts []domain.CategoryCount
		err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL hnsw.iterative_scan = relaxed_order`); err != nil {
				return fmt.Errorf("set iterative scan: %w", err)
			}
			rows, err := tx.Query(ctx, q.hybrid(in)+`
SELECT `+column+`, count(*) FROM fused AS f JOIN listings AS l ON l.id = f.id GROUP BY 1 ORDER BY 2 DESC, 1`, q.args)
			if err != nil {
				return fmt.Errorf("hybrid counts: %w", err)
			}
			if counts, err = pgx.CollectRows(rows, pgx.RowToStructByPos[domain.CategoryCount]); err != nil {
				return fmt.Errorf("read counts: %w", err)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("count: %w", err)
		}
		return counts, nil
	}
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
	counts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.CategoryCount])
	if err != nil {
		return nil, fmt.Errorf("read counts: %w", err)
	}
	return counts, nil
}

func (s pgSearches) hybridSearch(ctx context.Context, in domain.Search) ([]domain.Hit, error) {
	q := query{args: pgx.StrictNamedArgs{"limit": in.Limit, "offset": in.Offset}}
	sql := q.hybrid(in) + `
SELECT l.id, l.lat, l.lng FROM fused AS f JOIN listings AS l ON l.id = f.id`
	switch in.Sort { //nolint:exhaustive // relevance is the default order
	case domain.SortNewest:
		sql += ` ORDER BY l.published_at DESC, l.id DESC`
	case domain.SortPriceAsc:
		sql += ` ORDER BY l.price ASC, l.id`
	case domain.SortPriceDesc:
		sql += ` ORDER BY l.price DESC, l.id`
	case domain.SortNearest:
		sql += ` ORDER BY ` + distanceSQL + `, l.id`
	default:
		sql += ` ORDER BY f.score DESC, l.id`
	}
	var hits []domain.Hit
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// The vector lists filter after the index; iterative scans keep
		// reading the index until a list is full or max_scan_tuples is read.
		if _, err := tx.Exec(ctx, `SET LOCAL hnsw.iterative_scan = relaxed_order`); err != nil {
			return fmt.Errorf("set iterative scan: %w", err)
		}
		rows, err := tx.Query(ctx, sql+` LIMIT @limit OFFSET @offset`, q.args)
		if err != nil {
			return fmt.Errorf("hybrid search: %w", err)
		}
		if hits, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Hit, error) {
			var h domain.Hit
			return h, row.Scan(&h.ID, &h.Point.Lat, &h.Point.Lng)
		}); err != nil {
			return fmt.Errorf("read hits: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return hits, nil
}
