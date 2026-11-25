package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/chotot"
)

// SyncService is the ChototSync service's name without the prefix.
const SyncService = "ChototSync"

// maxConfirms is how many copies one check asks Chợ Tốt about one by one,
// when their listings are no longer in the profile's.
const maxConfirms = 20

// SyncInput is a check of a link; Failures counts the checks before it in
// a row that Chợ Tốt did not answer.
type SyncInput struct {
	Link     uuid.UUID `json:"link"`
	Failures int       `json:"failures,omitempty"`
}

// ChototSync checks one link per invocation and sends the next check,
// about SyncEvery later, later still while Chợ Tốt does not answer. A link
// gone ends the chain.
type ChototSync struct{ Deps }

func (s ChototSync) ServiceName() string { return s.Prefix + SyncService }

// checked is what a check found: the link, whether Chợ Tốt answered, and
// the new listings to bring over.
type checked struct {
	Found       bool      `json:"found"`
	Unavailable bool      `json:"unavailable,omitempty"`
	Account     uuid.UUID `json:"account"`
	New         []int64   `json:"new,omitempty"`
}

// Run checks the link in one step; with auto_import, it brings the new
// listings over, waiting for each, and announces how many it listed.
func (s ChototSync) Run(ctx restate.Context, in SyncInput) error {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return err
	}
	c, err := restate.Run(ctx, func(rc restate.RunContext) (checked, error) { return s.check(rc, in.Link, at) })
	if err != nil {
		return fmt.Errorf("check link %s: %w", in.Link, err)
	}
	if !c.Found {
		return nil
	}
	if c.Unavailable {
		s.next(ctx, in.Link, in.Failures+1)
		return nil
	}
	if len(c.New) > 0 {
		started, ids, err := s.startImports(ctx, c.Account, c.New, at)
		if err != nil {
			return err
		}
		listed := 0
		for i := range started.Imports {
			imp := &started.Imports[i]
			if imp.ID != ids[i] {
				continue
			}
			res, err := restate.Service[ImportResult](ctx, s.Prefix+ImportService, "Run").
				Request(imp.ID, restate.WithIdempotencyKey(imp.ID.String()))
			if err != nil {
				return fmt.Errorf("import %d: %w", imp.ListID, err)
			}
			if res.State == domain.Listed {
				listed++
			}
		}
		if listed > 0 {
			ev := restatex.NewID(ctx, at)
			if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
				return s.Links.Imported(rc, in.Link, listed, ev, at)
			}); err != nil {
				return fmt.Errorf("announce imports: %w", err)
			}
		}
	}
	s.next(ctx, in.Link, 0)
	return nil
}

// next sends the link's next check.
func (s ChototSync) next(ctx restate.Context, link uuid.UUID, failures int) {
	wait := domain.NextSync(s.SyncEvery, failures, restate.Rand(ctx).Float64())
	restate.ServiceSend(ctx, s.ServiceName(), "Run").Send(SyncInput{Link: link, Failures: failures}, restate.WithDelay(wait))
}

// check reads the link's listings on Chợ Tốt; with follow_updates, it
// hides and reprices the copies; with auto_import, it picks the listings
// newer than any seen, as many as the cap leaves room for. It records the
// check unless Chợ Tốt did not answer.
func (d Deps) check(ctx context.Context, id uuid.UUID, at time.Time) (checked, error) {
	l, found, err := d.Links.ByID(ctx, id)
	if err != nil || !found {
		return checked{}, err //nolint:wrapcheck // the caller names the step
	}
	adsCtx, cancel := context.WithTimeout(ctx, d.CallTimeout)
	ads, err := d.Source.Ads(adsCtx, l.ChototAccount, l.OID)
	cancel()
	if err != nil {
		if errors.Is(err, chotot.ErrFormat) {
			d.Log.ErrorContext(ctx, "chotot format: the gateway's answer changed", "link", id, "err", err)
		}
		d.Log.WarnContext(ctx, "chotot check: Chợ Tốt did not answer", "link", id, "err", err)
		return checked{Found: true, Unavailable: true}, nil
	}
	imps, err := d.Imports.List(ctx, l.AccountID)
	if err != nil {
		return checked{}, err //nolint:wrapcheck // the caller names the step
	}
	if l.FollowUpdates {
		if err = d.follow(ctx, l, ads, imps); err != nil {
			return checked{}, err
		}
	}
	statuses, room, err := d.statuses(ctx, l.AccountID, ads, imps, at)
	if err != nil {
		return checked{}, err
	}
	out := checked{Found: true, Account: l.AccountID}
	waiting, seen := 0, int64(0)
	for i, ad := range ads {
		seen = max(seen, ad.ListID)
		if statuses[i].State != domain.AdImportable {
			continue
		}
		waiting++
		if l.AutoImport && l.SeenThrough != nil && ad.ListID > *l.SeenThrough && len(out.New) < room {
			out.New = append(out.New, ad.ListID)
		}
	}
	if err = d.Links.Checked(ctx, id, at, waiting-len(out.New), seen); err != nil {
		return checked{}, err //nolint:wrapcheck // the caller names the step
	}
	d.Log.InfoContext(ctx, "chotot check", "link", id, "ads", len(ads), "waiting", waiting, "new", len(out.New))
	return out, nil
}

// follow brings the link's copies in line with their listings on Chợ Tốt:
// a new price reprices the copy, and a listing Chợ Tốt says is gone or not
// active hides it. A listing missing from ads is asked about alone, at most
// maxConfirms a check, and only a definite answer hides its copy.
func (d Deps) follow(ctx context.Context, l domain.Link, ads []chotot.Listed, imps []domain.Import) error {
	listed := make(map[int64]chotot.Listed, len(ads))
	for _, ad := range ads {
		listed[ad.ListID] = ad
	}
	confirms := 0
	for k := range imps {
		imp := &imps[k]
		if imp.State != domain.Listed || imp.OID != l.OID || imp.AdGone || imp.ListingID == nil {
			continue
		}
		if ad, ok := listed[imp.ListID]; ok {
			if ad.Price == imp.AdPrice {
				continue
			}
			if ad.Price >= catalogdomain.MinPrice && ad.Price <= catalogdomain.MaxPrice {
				if err := d.Catalog.Reprice(ctx, l.AccountID, *imp.ListingID, ad.Price); err != nil && !restatex.IsRefusal(err) {
					return fmt.Errorf("reprice %s: %w", *imp.ListingID, err)
				}
			}
			if err := d.Imports.Followed(ctx, imp.ID, ad.Price, false); err != nil {
				return err //nolint:wrapcheck // the caller names the step
			}
			continue
		}
		if confirms == maxConfirms {
			continue
		}
		confirms++
		adCtx, cancel := context.WithTimeout(ctx, d.CallTimeout)
		_, err := d.Source.Ad(adCtx, imp.ListID)
		cancel()
		if !errors.Is(err, chotot.ErrNotFound) {
			continue
		}
		if _, err = d.Catalog.Hide(ctx, l.AccountID, *imp.ListingID, locale.Vietnamese); err != nil && !restatex.IsRefusal(err) {
			return fmt.Errorf("hide %s: %w", *imp.ListingID, err)
		}
		if err = d.Imports.Followed(ctx, imp.ID, imp.AdPrice, true); err != nil {
			return err //nolint:wrapcheck // the caller names the step
		}
	}
	return nil
}

// statuses is what each of ads is here for account, and how many more
// listings the account may put on sale.
func (d Deps) statuses(ctx context.Context, account uuid.UUID, ads []chotot.Listed, imps []domain.Import, at time.Time) ([]domain.AdStatus, int, error) {
	onSale, err := d.Catalog.OnSale(ctx, account)
	if err != nil {
		return nil, 0, fmt.Errorf("list listings on sale: %w", err)
	}
	sums, err := d.Catalog.Summaries(ctx, onSale)
	if err != nil {
		return nil, 0, fmt.Errorf("read listings on sale: %w", err)
	}
	mine := make([]domain.Mine, 0, len(onSale))
	for _, id := range onSale {
		if s, ok := sums[id]; ok {
			mine = append(mine, domain.Mine{ID: id, Title: s.Title, Price: s.Price})
		}
	}
	byList := make(map[int64]*domain.Import, len(imps))
	for i := range imps {
		byList[imps[i].ListID] = &imps[i]
	}
	out := make([]domain.AdStatus, len(ads))
	for i, ad := range ads {
		out[i] = domain.StatusOf(chotot.RealEstate(ad.Category), byList[ad.ListID], domain.SameItem(ad.Subject, ad.Price, mine), at)
	}
	return out, max(0, catalogdomain.MaxOnSale-len(onSale)), nil
}
