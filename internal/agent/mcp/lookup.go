package mcp

import (
	"context"
	"fmt"
	"uuid"

	"golang.org/x/sync/errgroup"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/authn"
)

// finder reads what one kind of record says of id in a few words, ok false
// when no record of the kind has it. A finder reads nobody's words, so
// lookup records no read.
type finder func(ctx context.Context, t tools, id uuid.UUID) (label string, ok bool, err error)

// finders are the kinds lookup tries, each through the module that owns
// it; a new kind is one more entry. A report case's id is its account's.
var finders = []struct {
	kind, tool string
	find       finder
}{
	{"account", "account_get", func(ctx context.Context, t tools, id uuid.UUID) (string, bool, error) {
		a, err := t.d.Accounts.ConsoleAccount(ctx, id)
		if err != nil {
			return "", false, fmt.Errorf("read the account: %w", err)
		}
		return fmt.Sprintf("%s, %s", a.DisplayName, a.Status), true, nil
	}},
	{"report_case", "report_case_get", func(ctx context.Context, t tools, id uuid.UUID) (string, bool, error) {
		c, err := t.d.Trust.ReportCase(ctx, id)
		if err != nil {
			return "", false, fmt.Errorf("read the case: %w", err)
		}
		if c.Resolution != nil {
			return fmt.Sprintf("resolved (%s), %d reports", c.Resolution.Outcome, len(c.Reports)), true, nil
		}
		return fmt.Sprintf("open, %d reports", len(c.Reports)), true, nil
	}},
	{"order", "order_get", func(ctx context.Context, t tools, id uuid.UUID) (string, bool, error) {
		o, ok, err := t.d.Deals.Order(ctx, id)
		if err != nil {
			return "", false, fmt.Errorf("read the order: %w", err)
		}
		if !ok {
			return "", false, nil
		}
		return fmt.Sprintf("%s, %s", o.Code, o.Status), true, nil
	}},
	{"listing", "listing_get", func(ctx context.Context, t tools, id uuid.UUID) (string, bool, error) {
		l, ok, err := t.d.Catalog.Summary(ctx, id)
		if err != nil {
			return "", false, fmt.Errorf("read the listing: %w", err)
		}
		if !ok {
			return "", false, nil
		}
		return string(l.Status), true, nil
	}},
	{"ticket", "ticket_get", func(ctx context.Context, t tools, id uuid.UUID) (string, bool, error) {
		d, err := t.d.Support.Detail(ctx, id)
		if err != nil {
			return "", false, fmt.Errorf("read the ticket: %w", err)
		}
		return fmt.Sprintf("%s, %s", d.Ticket.Ticket.Kind, d.Ticket.Ticket.Status), true, nil
	}},
}

type lookupIn struct {
	ID uuid.UUID `json:"id" jsonschema:"any id seen in ShopNexus"`
}

type lookupOut struct {
	ID      uuid.UUID `json:"id"`
	Matches []match   `json:"matches" jsonschema:"the records with this id; empty when there are none"`
}

type match struct {
	Kind  string `json:"kind"  jsonschema:"account, report_case, order, listing or ticket"`
	Label string `json:"label" jsonschema:"a few words on the record, never what a person wrote"`
	Tool  string `json:"tool"  jsonschema:"the tool that reads the record"`
}

// lookup tries every finder at once; a not-found answer is no match, and
// any other failure fails the call.
func (t tools) lookup(ctx context.Context, _ authn.ConsoleMember, in lookupIn) (lookupOut, error) {
	// Each finder writes its own slot, so the order is the finders'.
	found := make([]*match, len(finders))
	g, gctx := errgroup.WithContext(ctx)
	for i, f := range finders {
		g.Go(func() error {
			label, ok, err := f.find(gctx, t, in.ID)
			if err != nil && apperr.From(err).Kind == apperr.KindNotFound {
				return nil
			}
			if err != nil {
				return fmt.Errorf("look up a %s: %w", f.kind, err)
			}
			if ok {
				found[i] = &match{Kind: f.kind, Label: label, Tool: f.tool}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return lookupOut{}, err
	}
	out := lookupOut{ID: in.ID, Matches: []match{}}
	for _, m := range found {
		if m != nil {
			out.Matches = append(out.Matches, *m)
		}
	}
	return out, nil
}
