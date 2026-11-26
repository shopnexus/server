package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/provider/chotot"
)

// Code is UC-LST-04, step 1: it returns the account's live code, or a new
// one.
func (c *Chotot) Code(ctx context.Context, account uuid.UUID) (domain.Code, error) {
	now := c.d.Now()
	code, err := c.d.Codes.Issue(ctx, account, domain.NewCode(now), now)
	if err != nil {
		return domain.Code{}, err //nolint:wrapcheck // the store names it
	}
	return code, nil
}

// Link is UC-LST-04, steps 2 and 3: it checks, in order, the address, a
// live code, the profile on Chợ Tốt, then the code in its bio. It reads the
// profile's listings, so that only newer ones are brought over on their
// own, and starts the link's checks before it stores the link.
func (c *Chotot) Link(ctx context.Context, account uuid.UUID, profileURL string) (LinkView, error) {
	// E1
	oid, err := domain.ParseProfileURL(profileURL)
	if err != nil {
		return LinkView{}, err
	}
	now := c.d.Now()
	code, found, err := c.d.Codes.Get(ctx, account)
	if err != nil {
		return LinkView{}, err //nolint:wrapcheck // the store names it
	}
	// E2
	if !found || !code.Live(now) {
		return LinkView{}, domain.ErrCodeExpired
	}
	// E3, E4
	profileCtx, cancel := context.WithTimeout(ctx, c.d.CallTimeout)
	profile, err := c.d.Source.Profile(profileCtx, oid)
	cancel()
	switch {
	case errors.Is(err, chotot.ErrNotFound):
		return LinkView{}, domain.ErrProfileNotFound
	case errors.Is(err, chotot.ErrFormat):
		c.d.Log.ErrorContext(ctx, "chotot format: the profile's answer changed", "err", err)
		return LinkView{}, domain.ErrUnavailable.WithCause(err)
	case err != nil:
		return LinkView{}, domain.ErrUnavailable.WithCause(err)
	}
	// E5
	if !domain.Holds(profile.Description, code.Code) {
		return LinkView{}, domain.ErrCodeNotFound.WithProfile(apperr.Profile{Name: profile.Name, AvatarURL: nonEmpty(profile.AvatarURL)})
	}
	l := domain.NewLink(uuid.NewV7(), account, oid, profile.AccountID, profile.Name, profile.AvatarURL, now)
	imps, err := c.d.Imports.List(ctx, account)
	if err != nil {
		return LinkView{}, err //nolint:wrapcheck // the store names it
	}
	// Without the listings, the first check takes the newest as seen.
	adsCtx, cancel := context.WithTimeout(ctx, c.d.CallTimeout)
	ads, err := c.d.Source.Ads(adsCtx, profile.AccountID, oid)
	cancel()
	if err == nil {
		statuses, _, statusErr := c.d.statuses(ctx, account, ads, imps, now)
		if statusErr != nil {
			return LinkView{}, statusErr
		}
		seen, waiting := int64(0), 0
		for i, ad := range ads {
			seen = max(seen, ad.ListID)
			if statuses[i].State == domain.AdImportable {
				waiting++
			}
		}
		l.SeenThrough, l.Waiting = &seen, &waiting
	} else {
		c.d.Log.WarnContext(ctx, "chotot link: the listings were not read", "err", err)
	}
	if err = c.d.Checks.Schedule(ctx, l.ID); err != nil {
		return LinkView{}, fmt.Errorf("start the checks: %w", err)
	}
	// 3. Store the link and use up the code; A2: the link moves from any
	// other account that linked the profile.
	if l, err = c.d.Links.Link(ctx, l, code.Code); err != nil {
		return LinkView{}, err //nolint:wrapcheck // a refusal, or the store names it
	}
	return linkView(l, imported(imps, oid, now)), nil
}

// Get is UC-LST-04: it returns the account's link, nil for none.
func (c *Chotot) Get(ctx context.Context, account uuid.UUID) (StateView, error) {
	l, found, err := c.d.Links.Get(ctx, account)
	if err != nil || !found {
		return StateView{}, err //nolint:wrapcheck // the store names it
	}
	v, err := c.view(ctx, l)
	if err != nil {
		return StateView{}, err
	}
	return StateView{Link: &v}, nil
}

// Update is UC-LST-04 A6: it changes the link's settings.
func (c *Chotot) Update(ctx context.Context, account uuid.UUID, p domain.SettingsPatch) (LinkView, error) {
	l, found, err := c.d.Links.Update(ctx, account, p)
	if err != nil {
		return LinkView{}, err //nolint:wrapcheck // the store names it
	}
	if !found {
		return LinkView{}, domain.ErrNotLinked
	}
	return c.view(ctx, l)
}

// Unlink is UC-LST-04 A6: it removes the link; the chain of checks ends at
// its next check.
func (c *Chotot) Unlink(ctx context.Context, account uuid.UUID) error {
	return c.d.Links.Delete(ctx, account) //nolint:wrapcheck // the store names it
}

// view is l with the account's count of imports.
func (c *Chotot) view(ctx context.Context, l domain.Link) (LinkView, error) {
	imps, err := c.d.Imports.List(ctx, l.AccountID)
	if err != nil {
		return LinkView{}, err //nolint:wrapcheck // the store names it
	}
	return linkView(l, imported(imps, l.OID, c.d.Now())), nil
}
