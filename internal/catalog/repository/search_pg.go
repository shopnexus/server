package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/pgvec"
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

// textFields are the index's title, description and specs fields for p:
// as written, or with diacritics folded away (migration 0076).
func textFields(p domain.Phrase) (title, description, specs string) {
	if p.Folded {
		return "'title_folded'", "'description_folded'", "'specs_folded'"
	}
	return "'title'", "'description'", "'specs_text'"
}

// phrase matches the title at twice its weight, description and specs at
// its weight, and the title's long words with one edit at half of it, in
// the fields textFields names.
func (q *query) phrase(p domain.Phrase) []string {
	text := q.arg(p.Text, "text")
	title, description, specs := textFields(p)
	out := []string{
		`paradedb.boost(` + q.arg(2*p.Weight, "real") + `, paradedb.match(` + title + `, ` + text + `))`,
		`paradedb.boost(` + q.arg(p.Weight, "real") + `, paradedb.match(` + description + `, ` + text + `))`,
		`paradedb.boost(` + q.arg(p.Weight, "real") + `, paradedb.match(` + specs + `, ` + text + `))`,
	}
	var long []string
	for w := range strings.FieldsSeq(p.Text) {
		if utf8.RuneCountInString(w) >= fuzzyRunes {
			long = append(long, w)
		}
	}
	if len(long) > 0 {
		out = append(out, `paradedb.boost(`+q.arg(p.Weight/2, "real")+`, paradedb.match(`+title+`, `+
			q.arg(strings.Join(long, " "), "text")+`, distance => 1))`)
	}
	return out
}

// allWords matches a listing holding every word of p, each in its title,
// description or specs. A word is a run of letters and digits; a phrase of
// none matches nothing.
func (q *query) allWords(p domain.Phrase) string {
	fields := strings.FieldsFunc(p.Text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	words := make([]string, 0, len(fields))
	title, description, specs := textFields(p)
	for _, w := range fields {
		text := q.arg(w, "text")
		words = append(words, `paradedb.boolean(should => `+array([]string{
			`paradedb.match(` + title + `, ` + text + `)`,
			`paradedb.match(` + description + `, ` + text + `)`,
			`paradedb.match(` + specs + `, ` + text + `)`,
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

// where is the search's condition, the index's and the plain ones; counts
// leave the boosts out, since they change no membership.
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
	if ex := q.plain(s, ""); len(ex) > 0 {
		sql += ` AND ` + strings.Join(ex, " AND ")
	}
	return sql
}

// distanceSQL is the great-circle distance in km from the origin parameters.
const distanceSQL = `(12742 * asin(sqrt(power(sin(radians(lat - @olat) / 2), 2)
       + cos(radians(@olat)) * cos(radians(lat)) * power(sin(radians(lng - @olng) / 2), 2))))`

// photosPerListing is how many photo rows the photo list reads for each
// listing it keeps, before it groups them by listing.
const photosPerListing = 4

// A relevance page needs the top of each list; a listing deep in every list
// rarely outranks the page. Short lists keep the top-K scans fast: ParadeDB's
// BM25 took 172 ms at 500 and 44 ms at 100.
const (
	listDepthFactor = 3
	minListDepth    = 60
)

// listDepth is how many listings each list of a hybrid search reads.
func listDepth(s domain.Search) int {
	switch s.Sort { //nolint:exhaustive // relevance and the default read by the page
	case domain.SortNewest, domain.SortPriceAsc, domain.SortPriceDesc, domain.SortNearest:
		return domain.MaxResults
	default:
		return min(max(listDepthFactor*(s.Offset+s.Limit), minListDepth), domain.MaxResults)
	}
}

// filters is the person's filters and the plain conditions as SQL on the
// listings alias l, for the vector lists. The BM25 list gets the same
// filters through must and plain.
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
		out = append(out, `l.condition = ANY(`+q.arg(f.Conditions, "text[]")+`)`)
	}
	if f.OffersOnly {
		out = append(out, `l.allow_offers`)
	}
	if f.Origin != nil && f.RadiusKm > 0 {
		out = append(out, distanceSQL+` <= @radius`)
	}
	return strings.Join(append(out, q.plain(s, "l.")...), " AND ")
}

// plain is the conditions the index does not hold, as plain SQL on the
// listings alias: the province, the similar-listings source, the IDs filter
// and the listings and sellers left out.
func (q *query) plain(s domain.Search, alias string) []string {
	var out []string
	if s.Filters.Province != "" {
		out = append(out, alias+`commune_code IN (SELECT code FROM communes WHERE province_code = `+q.arg(s.Filters.Province, "text")+`)`)
	}
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
// most depth listings ranked from 1, and fused, the weighted RRF score of
// every listing a list found.
func (q *query) hybrid(s domain.Search, depth int) string {
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
		q.args["list"] = depth
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
			pgvec.Format(s.Vectors.Photo), s.Vectors.PhotoVersion, 1-s.Vectors.PhotoMin, photosPerListing*depth
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
	if in.Sort == domain.SortPointsNewest {
		return s.pointsNewest(ctx, in)
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
	return collectHits(rows)
}

// pointsNewest runs a SortPointsNewest search over a bounded set of
// candidates. Every listing of one subcategory has the same points, so a
// listing in the first offset+limit of the order is among the newest
// offset+limit of its boosted subcategory, or of the listings outside
// every boosted one. One branch per boosted subcategory and one for the
// rest read those through the index, and the outer query orders them.
// The branches share one binding of the ID lists the search leaves out.
func (s pgSearches) pointsNewest(ctx context.Context, in domain.Search) ([]domain.Hit, error) {
	q := query{args: pgx.StrictNamedArgs{"limit": in.Limit, "offset": in.Offset, "branch": in.Offset + in.Limit}}
	var codes []string
	for _, b := range in.Boosts {
		if b.Kind != domain.BoostCategory {
			return nil, fmt.Errorf("sort %s takes category boosts only, not %s", in.Sort, b.Kind)
		}
		if !slices.Contains(codes, b.Code) {
			codes = append(codes, b.Code)
		}
	}
	var shared string
	if ex := q.plain(in, ""); len(ex) > 0 {
		shared = ` AND ` + strings.Join(ex, " AND ")
	}
	if o := in.Filters.Origin; o != nil && in.Filters.RadiusKm > 0 {
		q.args["olat"], q.args["olng"], q.args["radius"] = o.Lat, o.Lng, in.Filters.RadiusKm
		shared += ` AND ` + distanceSQL + ` <= @radius`
	}
	branch := func(f domain.Search, cond string) string {
		return `(SELECT id FROM listings WHERE id @@@ paradedb.boolean(must => ` + array(q.must(f)) + `)` + shared + cond +
			` ORDER BY published_at DESC, id DESC LIMIT @branch)`
	}
	branches := make([]string, 0, len(codes)+1)
	for _, code := range codes {
		if in.Filters.CategoryCode != "" && in.Filters.CategoryCode != code {
			continue // the filter leaves this subcategory out
		}
		f := in
		f.Filters.CategoryCode = code
		branches = append(branches, branch(f, ""))
	}
	rest := ""
	if len(codes) > 0 {
		rest = ` AND category_code <> ALL(` + q.arg(codes, "text[]") + `)`
	}
	branches = append(branches, branch(in, rest))
	// The cast keeps the points of no boost, a bare 0, from reading as a
	// column position.
	rows, err := s.db.Query(ctx, `SELECT l.id, l.lat, l.lng FROM listings AS l JOIN (`+strings.Join(branches, " UNION ALL ")+`) AS c ON c.id = l.id
ORDER BY (`+q.points(in.Boosts)+`)::float8 DESC, l.published_at DESC, l.id DESC LIMIT @limit OFFSET @offset`, q.args)
	if err != nil {
		return nil, fmt.Errorf("search by points: %w", err)
	}
	return collectHits(rows)
}

// collectHits reads rows of a listing's ID and point.
func collectHits(rows pgx.Rows) ([]domain.Hit, error) {
	hits, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Hit, error) {
		var h domain.Hit
		return h, row.Scan(&h.ID, &h.Point.Lat, &h.Point.Lng)
	})
	if err != nil {
		return nil, fmt.Errorf("read hits: %w", err)
	}
	return hits, nil
}

// iterativeScan queues the setting the hybrid lists need: they filter after
// the vector index, so the scan keeps reading it until a list is full. The
// local value lasts until the batch's implicit transaction ends.
func iterativeScan(b *pgx.Batch) {
	b.Queue(`SELECT set_config('hnsw.iterative_scan', 'relaxed_order', true)`)
}

func (s pgSearches) Counts(ctx context.Context, in domain.Search) ([]domain.CategoryCount, error) {
	if in.Hybrid() {
		return nil, errors.New("counts take a search without lists to fuse")
	}
	q := query{args: pgx.StrictNamedArgs{}}
	rows, err := s.db.Query(ctx, `SELECT category_code, count(*) FROM listings WHERE `+q.where(in, false)+
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
	sql := q.hybrid(in, listDepth(in)) + `
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
	b := &pgx.Batch{}
	iterativeScan(b)
	b.Queue(sql+` LIMIT @limit OFFSET @offset`, q.args).Query(func(rows pgx.Rows) error {
		var err error
		hits, err = collectHits(rows)
		return err
	})
	if err := s.db.SendBatch(ctx, b).Close(); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return hits, nil
}

// Words runs text through the tokenizer the listings index gives its text
// fields, so a listing not posted yet is matched word for word as one in
// the index would be.
func (s pgSearches) Words(ctx context.Context, text string) ([]string, error) {
	var words []string
	if err := s.db.QueryRow(ctx, `SELECT coalesce((@text::text)::pdb.unicode_words::text[], '{}')`,
		pgx.StrictNamedArgs{"text": text}).Scan(&words); err != nil {
		return nil, fmt.Errorf("split words: %w", err)
	}
	return words, nil
}
