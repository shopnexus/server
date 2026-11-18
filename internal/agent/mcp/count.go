package mcp

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/authn"
)

// counter counts one kind of record created at or after a time, the zero
// time for all, by status.
type counter func(ctx context.Context, since time.Time) (map[string]int, error)

// counted adapts a module's count, keyed by its own status type.
func counted[S ~string](f func(context.Context, time.Time) (map[S]int, error)) counter {
	return func(ctx context.Context, since time.Time) (map[string]int, error) {
		n, err := f(ctx, since)
		if err != nil {
			return nil, err
		}
		out := make(map[string]int, len(n))
		for k, v := range n {
			out[string(k)] = v
		}
		return out, nil
	}
}

// counterOf names a counter and picks it from the services.
type counterOf struct {
	entity string
	of     func(Deps) counter
}

// counters is what count counts: one entry per entity, the entity's name
// being the value count takes. A new entity is one more entry here and a
// count in the module that owns it.
var counters = []counterOf{
	{"accounts", func(d Deps) counter { return counted(d.Accounts.CountByStatus) }},
	{"listings", func(d Deps) counter { return counted(d.Catalog.CountByStatus) }},
	{"orders", func(d Deps) counter { return counted(d.Deals.CountOrders) }},
	{"tickets", func(d Deps) counter { return counted(d.Support.CountByStatus) }},
	{"report_cases", func(d Deps) counter { return counted(d.Trust.CountReportCases) }},
}

// entity is the name of one of counters.
type entity string

// entities lists the counters' names, the enum of entity's schema.
func entities() []any {
	out := make([]any, 0, len(counters))
	for _, c := range counters {
		out = append(out, c.entity)
	}
	return out
}

const countDescription = "How many records of a kind ShopNexus has, by status, with the total: the answer to " +
	`"how many …" questions. accounts (active, suspended, deleted; by sign-up time, ShopNexus's own account aside), ` +
	"listings (processing, active, hidden, rejected, reserved, sold, deleted; by creation time), orders (pending, " +
	"shipped, completed, failed, cancelled; by request time), tickets (open, resolved; by opening time), " +
	"report_cases (open: accounts with an open report made since; resolved: cases resolved since). " +
	"since narrows to records from that time on; without it, all time. Statuses with none are left out."

type countIn struct {
	Entity entity     `json:"entity"          jsonschema:"what to count"`
	Since  *time.Time `json:"since,omitempty" jsonschema:"count only records from this time on, RFC 3339 with an offset; leave out for all time"`
}

type countOut struct {
	Entity   string         `json:"entity"`
	Since    *time.Time     `json:"since,omitempty"`
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"by_status"`
}

// count reads the counter that in.Entity names; none of it is personal.
func (t tools) count(ctx context.Context, _ authn.ConsoleMember, in countIn) (countOut, error) {
	i := slices.IndexFunc(counters, func(c counterOf) bool { return c.entity == string(in.Entity) })
	if i < 0 {
		return countOut{}, apperr.New(apperr.KindBadRequest, "bad_request", fmt.Sprintf("unknown entity %q", in.Entity))
	}
	var since time.Time
	if in.Since != nil {
		since = *in.Since
	}
	n, err := counters[i].of(t.d)(ctx, since)
	if err != nil {
		return countOut{}, fmt.Errorf("count %s: %w", in.Entity, err)
	}
	out := countOut{Entity: string(in.Entity), Since: in.Since, ByStatus: n}
	for _, v := range n {
		out.Total += v
	}
	return out, nil
}
