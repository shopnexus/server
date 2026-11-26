// Package service is module chotot's use cases: link a seller's Chợ Tốt
// profile, list its listings, bring them over as listings, and follow
// them.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	assistsvc "github.com/shopnexus/server/internal/assist/service"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/chotot/repository"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/chotot"
)

// Service is module chotot's contract.
type Service interface {
	// Code returns the account's live code, or a new one for
	// domain.CodeLife.
	//
	// UC-LST-04
	Code(ctx context.Context, account uuid.UUID) (domain.Code, error)
	// Link links the profile at profileURL when its bio holds the account's
	// live code, and starts its hourly checks.
	//
	// UC-LST-04
	Link(ctx context.Context, account uuid.UUID, profileURL string) (LinkView, error)
	// Get returns the account's link; a nil Link for none. It reads
	// ShopNexus only.
	//
	// UC-LST-04
	Get(ctx context.Context, account uuid.UUID) (StateView, error)
	// Update changes the link's settings; no link is domain.ErrNotLinked.
	//
	// UC-LST-04 A6
	Update(ctx context.Context, account uuid.UUID, p domain.SettingsPatch) (LinkView, error)
	// Unlink removes the account's link; its imports stay. None is fine.
	//
	// UC-LST-04 A6
	Unlink(ctx context.Context, account uuid.UUID) error
	// Ads lists the linked profile's listings on Chợ Tốt, each with what it
	// is here.
	//
	// UC-LST-04
	Ads(ctx context.Context, account uuid.UUID) (AdListView, error)
	// Import starts an import of each listing and returns them as they now
	// are; one settled already answers as it is.
	//
	// UC-LST-04 · NFR-IDEM-01
	Import(ctx context.Context, account uuid.UUID, listIDs []int64) (ImportListView, error)
	// PostDraft posts the listing an import left as a draft, as in.
	//
	// UC-LST-04 A1
	PostDraft(ctx context.Context, account uuid.UUID, listID int64, in catalogdomain.ListingInput) (ImportView, error)
	// FromChotot says which of the account's listings an import brought
	// over from Chợ Tốt.
	FromChotot(ctx context.Context, account uuid.UUID, listings []uuid.UUID) (map[uuid.UUID]bool, error)
	// ListingSold announces chotot_import.sold, as event, when the listing
	// is an import's copy whose seller wants the reminder and whose listing
	// is still on Chợ Tốt.
	//
	// UC-LST-04 A4
	ListingSold(ctx context.Context, listing, event uuid.UUID, at time.Time) error
	// ListingEdited clears the review of the import that posted the
	// listing: the seller has seen it.
	ListingEdited(ctx context.Context, listing uuid.UUID) error
	// ListingDeleted forgets the import that posted the listing, so its
	// listing on Chợ Tốt may be brought over again.
	ListingDeleted(ctx context.Context, listing uuid.UUID) error
	// DeleteAccount removes a deleted account's code, link and imports.
	DeleteAccount(ctx context.Context, account uuid.UUID) error
	// LinkFacts is what notification tells of a check that brought
	// listings over; false for a gone link.
	LinkFacts(ctx context.Context, link uuid.UUID) (LinkFacts, bool, error)
	// SaleFacts is what notification tells of a sold copy; false for a gone
	// import.
	SaleFacts(ctx context.Context, imp uuid.UUID) (SaleFacts, bool, error)
}

// Source reads Chợ Tốt.
type Source interface {
	Profile(ctx context.Context, oid string) (chotot.Profile, error)
	Ads(ctx context.Context, accountID int64, oid string) ([]chotot.Listed, error)
	Ad(ctx context.Context, listID int64) (chotot.Ad, error)
	Image(ctx context.Context, url string) ([]byte, string, error)
}

// Filler writes a listing's draft from a Chợ Tốt listing: assist.
type Filler interface {
	FillAd(ctx context.Context, ad chotot.Ad) (assistsvc.AdFill, error)
}

// Scheduler starts a new link's chain of checks.
type Scheduler interface {
	Schedule(ctx context.Context, link uuid.UUID) error
}

// Deps is what New builds Chotot from.
type Deps struct {
	Codes    repository.Codes
	Links    repository.Links
	Imports  repository.Imports
	Source   Source
	Fill     Filler
	Accounts accountsvc.Service
	Catalog  catalogsvc.Service
	Media    mediasvc.Service
	Checks   Scheduler
	// CallTimeout bounds one call to Chợ Tốt; SyncEvery is how often a
	// link is checked, domain.SyncEvery in production.
	CallTimeout time.Duration
	SyncEvery   time.Duration
	Log         *slog.Logger
	// Ingress and Prefix reach this process's Restate services.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
}

// Chotot implements Service in this process.
type Chotot struct {
	d     Deps
	start restatex.Caller[StartInput, StartResult]
	post  restatex.Caller[DraftInput, DraftResult]
}

var _ Service = (*Chotot)(nil)

func New(d Deps) *Chotot {
	return &Chotot{
		d:     d,
		start: restatex.Caller[StartInput, StartResult]{Ingress: d.Ingress, Service: d.Prefix + ImportService, Handler: "Start"},
		post:  restatex.Caller[DraftInput, DraftResult]{Ingress: d.Ingress, Service: d.Prefix + ImportService, Handler: "PostDraft"},
	}
}

// Durable returns module chotot's Restate services, for Mount to bind.
func (c *Chotot) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{restate.Reflect(ChototImport{c.d}), restate.Reflect(ChototSync{c.d})}
}

type restateScheduler struct {
	ingress *ingress.Client
	name    string
	every   time.Duration
}

// NewScheduler sends the checks through in to the ChototSync of prefix,
// every apart.
func NewScheduler(in *ingress.Client, prefix string, every time.Duration) Scheduler {
	return restateScheduler{ingress: in, name: prefix + SyncService, every: every}
}

// Schedule sends the link's first check, every from now, keyed by the
// link, so a retry starts one chain. It runs before the row exists: no link
// is left without a chain, and a check that finds no row ends the chain.
func (s restateScheduler) Schedule(ctx context.Context, link uuid.UUID) error {
	if _, err := ingress.ServiceSend[SyncInput](s.ingress, s.name, "Run").
		Send(ctx, SyncInput{Link: link}, restate.WithDelay(s.every), restate.WithIdempotencyKey(link.String())); err != nil {
		return fmt.Errorf("schedule the checks: %w", err)
	}
	return nil
}
