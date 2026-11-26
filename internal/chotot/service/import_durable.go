package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/chotot/domain"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/chotot"
)

// ImportService is the ChototImport service's name without the prefix.
const ImportService = "ChototImport"

const (
	// fillAttempts is how many times the fill step reads the listing and
	// asks the model before the import is skipped as unavailable.
	fillAttempts = 3
	// photoAttempts is how many times the copy step asks the CDN for one
	// photo before it skips it.
	photoAttempts = 2
)

type StartInput struct {
	Account uuid.UUID `json:"account"`
	ListIDs []int64   `json:"list_ids"`
}

// StartResult is each listing's import as Start left it, or the refusal.
type StartResult struct {
	Imports []domain.Import `json:"imports"`
	Refused *apperr.Error   `json:"refused,omitempty"`
}

func (r StartResult) Refusal() *apperr.Error { return r.Refused }

type DraftInput struct {
	Account uuid.UUID `json:"account"`
	ListID  int64     `json:"list_id"`
	// Listing has no JSON names of its own; the journal keeps Go's.
	Listing catalogdomain.ListingInput `json:"listing"`
}

// DraftResult is the import PostDraft listed, or the refusal.
type DraftResult struct {
	Import  domain.Import `json:"import"`
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r DraftResult) Refusal() *apperr.Error { return r.Refused }

// ImportResult is the state Run left its import in.
type ImportResult struct {
	State domain.ImportState `json:"state"`
}

// ChototImport brings Chợ Tốt listings over. Start starts the imports, Run
// brings one listing over in steps, PostDraft posts one left as a draft.
// The import row is the dedupe key: one per account and listing, and only
// the run whose ID the row holds may save it.
type ChototImport struct{ Deps }

func (s ChototImport) ServiceName() string { return s.Prefix + ImportService }

// Start is UC-LST-04, step 5: it starts the imports in one step, then sends
// Run for each it started.
func (s ChototImport) Start(ctx restate.Context, in StartInput) (StartResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return StartResult{}, err
	}
	// E6; A5 · NFR-IDEM-01: an import already settled, or still running,
	// answers as it is and runs no second time.
	started, ids, err := s.startImports(ctx, in.Account, in.ListIDs, at)
	if err != nil || started.Refused != nil {
		return started, err
	}
	for i := range started.Imports {
		if id := started.Imports[i].ID; id == ids[i] {
			restate.ServiceSend(ctx, s.ServiceName(), "Run").Send(id, restate.WithIdempotencyKey(id.String()))
		}
	}
	return started, nil
}

// startImports starts account's imports of listIDs at at, in one step,
// under the IDs it returns, minted before the step: an import with its
// listing's ID is one this call started.
func (d Deps) startImports(ctx restate.Context, account uuid.UUID, listIDs []int64, at time.Time) (StartResult, []uuid.UUID, error) {
	ids := make([]uuid.UUID, len(listIDs))
	for i := range ids {
		ids[i] = restatex.NewID(ctx, at)
	}
	out, err := restate.Run(ctx, func(rc restate.RunContext) (StartResult, error) {
		l, found, stepErr := d.Links.Get(rc, account)
		if stepErr != nil {
			return StartResult{}, fmt.Errorf("read link: %w", stepErr)
		}
		if !found {
			return StartResult{Refused: domain.ErrNotLinked}, nil
		}
		imps := make([]domain.Import, len(listIDs))
		for i, id := range listIDs {
			imps[i] = domain.NewImport(ids[i], account, id, l.OID, at)
		}
		got, stepErr := d.Imports.Start(rc, imps, at)
		if stepErr != nil {
			refused, faultErr := restatex.AsRefusal(stepErr)
			if faultErr != nil {
				return StartResult{}, fmt.Errorf("start imports: %w", faultErr)
			}
			return StartResult{Refused: refused.Refusal}, nil
		}
		return StartResult{Imports: got}, nil
	})
	if err != nil {
		return StartResult{}, nil, fmt.Errorf("start imports: %w", err)
	}
	return out, ids, nil
}

// loadedImport is the load step's import and whether the account still
// links its profile.
type loadedImport struct {
	Import domain.Import `json:"import"`
	Found  bool          `json:"found"`
	Linked bool          `json:"linked"`
}

// filledAd is the fill step's draft, or the state and reason that end the
// import. The listing's words stay in the draft; nothing else keeps them.
type filledAd struct {
	State   domain.ImportState `json:"state,omitempty"`
	Reason  string             `json:"reason,omitempty"`
	Draft   domain.Draft       `json:"draft"`
	Guessed []string           `json:"guessed,omitempty"`
	Images  []string           `json:"images,omitempty"`
	AdPrice int64              `json:"ad_price"`
}

// copiedPhotos is what the copy step saved, in the listing's order, or the
// state and reason that end the import when it saved none.
type copiedPhotos struct {
	Uploads []uuid.UUID        `json:"uploads"`
	State   domain.ImportState `json:"state,omitempty"`
	Reason  string             `json:"reason,omitempty"`
}

// Run is UC-LST-04, step 5: it brings one listing over. It checks the
// seller may post, reads the listing and fills the draft, copies the
// photos, then posts it as POST /v1/listings does under the import's ID as
// the key. A listing the rules refuse stays a draft; any other refusal
// skips it.
func (s ChototImport) Run(ctx restate.Context, id uuid.UUID) (ImportResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return ImportResult{}, err
	}
	loaded, err := restate.Run(ctx, func(rc restate.RunContext) (loadedImport, error) {
		imp, found, stepErr := s.Imports.ByID(rc, id)
		if stepErr != nil || !found {
			return loadedImport{}, stepErr //nolint:wrapcheck // wrapped below
		}
		l, linked, stepErr := s.Links.Get(rc, imp.AccountID)
		if stepErr != nil {
			return loadedImport{}, stepErr //nolint:wrapcheck // wrapped below
		}
		return loadedImport{Import: imp, Found: true, Linked: linked && l.OID == imp.OID}, nil
	})
	if err != nil {
		return ImportResult{}, fmt.Errorf("load import: %w", err)
	}
	imp := loaded.Import
	if !loaded.Found || imp.State != domain.Importing {
		return ImportResult{State: imp.State}, nil
	}
	// E7: another profile's listing, a seller who may not post, a listing
	// gone or unreadable, or Chợ Tốt not answering skips the import.
	if !loaded.Linked {
		return s.end(ctx, imp, domain.Skipped, domain.ReasonNotYours, at)
	}
	ready, err := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) { return s.ready(rc, imp.AccountID) })
	if err != nil {
		return ImportResult{}, fmt.Errorf("check the seller: %w", err)
	}
	if ready.Refusal != nil {
		return s.end(ctx, imp, domain.Skipped, ready.Refusal.Code, at)
	}
	filled, err := restate.Run(ctx, func(rc restate.RunContext) (filledAd, error) {
		return s.fill(rc, imp)
	}, restate.WithMaxRetryAttempts(fillAttempts))
	if err != nil {
		s.Log.WarnContext(ctx, "chotot import: the listing was not read", "import", id, "list_id", imp.ListID, "err", err)
		return s.end(ctx, imp, domain.Skipped, domain.ReasonUnavailable, at)
	}
	if filled.State != "" {
		return s.end(ctx, imp, filled.State, filled.Reason, at)
	}
	commune, err := restate.Run(ctx, func(rc restate.RunContext) (string, error) { return s.commune(rc, imp.AccountID) })
	if err != nil {
		return ImportResult{}, fmt.Errorf("find the commune: %w", err)
	}
	photoIDs := make([]uuid.UUID, len(filled.Images))
	for i := range photoIDs {
		photoIDs[i] = restatex.NewID(ctx, at)
	}
	copied, err := restate.Run(ctx, func(rc restate.RunContext) (copiedPhotos, error) {
		return s.copyPhotos(rc, imp, filled.Images, photoIDs)
	}, restate.WithMaxRetryAttempts(fillAttempts))
	if err != nil {
		return s.end(ctx, imp, domain.Skipped, domain.ReasonUnavailable, at)
	}
	if copied.State != "" {
		return s.end(ctx, imp, copied.State, copied.Reason, at)
	}
	draft := filled.Draft
	draft.Photos, draft.CommuneCode = copied.Uploads, commune
	res, err := catalogsvc.PostFrom(ctx, s.Prefix, catalogsvc.PostInput{Seller: imp.AccountID, Key: imp.ID, Listing: draft.Listing()})
	if err != nil {
		return ImportResult{}, err //nolint:wrapcheck // PostFrom names the call
	}
	switch {
	case res.Refused == nil:
		imp.Listed(res.ListingID, filled.Guessed, filled.AdPrice, at)
	case errors.Is(res.Refused, catalogdomain.ErrInvalidListing):
		// A1 · BR-LST-03: the photos wait for the seller as long as the
		// draft does.
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
			return restatex.Ignore(s.Media.Extend(rc, imp.AccountID, draft.Photos), mediadomain.ErrInvalidUpload)
		}); err != nil {
			return ImportResult{}, fmt.Errorf("keep the draft's photos: %w", err)
		}
		imp.Drafted(draft, res.Refused, filled.AdPrice, at)
	default:
		imp.Ended(domain.Skipped, res.Refused.Code, at)
	}
	return s.save(ctx, imp, domain.Importing)
}

// end ends the import as state for reason.
func (s ChototImport) end(ctx restate.Context, imp domain.Import, state domain.ImportState, reason string, at time.Time) (ImportResult, error) {
	imp.Ended(state, reason, at)
	return s.save(ctx, imp, domain.Importing)
}

// save writes imp in a step while its row is still in state from; a row
// another run took over keeps that run's.
func (s ChototImport) save(ctx restate.Context, imp domain.Import, from domain.ImportState) (ImportResult, error) {
	saved, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) { return s.Imports.Save(rc, imp, from) })
	if err != nil {
		return ImportResult{}, fmt.Errorf("save import: %w", err)
	}
	if !saved {
		s.Log.InfoContext(ctx, "chotot import: another run took the import over", "import", imp.ID, "list_id", imp.ListID)
	}
	return ImportResult{State: imp.State}, nil
}

// ready is the refusal a post by account would meet before its fields: a
// suspended account, no phone, or the cap reached.
func (d Deps) ready(ctx context.Context, account uuid.UUID) (restatex.Refused, error) {
	status, err := d.Accounts.Status(ctx, account)
	if err != nil {
		return restatex.Refused{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return restatex.Refused{Refusal: r}, nil //nolint:nilerr // A refusal goes back as data.
	}
	hasPhone, err := d.Accounts.HasPhone(ctx, account)
	if err != nil {
		return restatex.Refused{}, fmt.Errorf("read the account's phone: %w", err)
	}
	if !hasPhone {
		return restatex.Refused{Refusal: accountdomain.ErrPhoneRequired}, nil
	}
	counts, err := d.Catalog.SellerCounts(ctx, account)
	if err != nil {
		return restatex.Refused{}, fmt.Errorf("count listings: %w", err)
	}
	if counts.OnSale >= catalogdomain.MaxOnSale {
		return restatex.Refused{Refusal: catalogdomain.ErrCapReached}, nil
	}
	return restatex.Refused{}, nil
}

// fill reads the import's listing and fills its draft.
func (d Deps) fill(ctx context.Context, imp domain.Import) (filledAd, error) {
	adCtx, cancel := context.WithTimeout(ctx, d.CallTimeout)
	ad, err := d.Source.Ad(adCtx, imp.ListID)
	cancel()
	switch {
	case errors.Is(err, chotot.ErrNotFound):
		return filledAd{State: domain.Skipped, Reason: domain.ReasonAdGone}, nil
	case errors.Is(err, chotot.ErrFormat):
		d.Log.ErrorContext(ctx, "chotot format: the gateway's answer changed", "import", imp.ID, "list_id", imp.ListID, "err", err)
		return filledAd{State: domain.Skipped, Reason: domain.ReasonUnavailable}, nil
	case err != nil:
		return filledAd{}, fmt.Errorf("read the listing: %w", err)
	case ad.AccountOID != imp.OID:
		return filledAd{State: domain.Skipped, Reason: domain.ReasonNotYours}, nil
	case chotot.RealEstate(ad.Category):
		return filledAd{State: domain.Unsupported, Reason: domain.ReasonRealEstate}, nil
	}
	f, err := d.Fill.FillAd(ctx, ad)
	if err != nil {
		return filledAd{}, fmt.Errorf("fill the draft: %w", err)
	}
	if f.NoItem != "" {
		return filledAd{State: domain.Unsupported, Reason: domain.ReasonNoItem}, nil
	}
	dr := domain.Draft{
		Title: f.Draft.Title, Price: f.Draft.Price, Description: f.Draft.Description, CategoryCode: f.Draft.CategoryCode,
		Condition: f.Draft.Condition, Specs: f.Draft.Specs,
	}
	if dr.Condition == "" {
		dr.Condition = catalogdomain.ConditionUsed
	}
	return filledAd{
		Draft: dr, Guessed: f.Guessed, Images: ad.Images[:min(len(ad.Images), catalogdomain.MaxPhotos)], AdPrice: max(ad.Price, 0),
	}, nil
}

// commune is where account's listings are: their default address's
// commune, else their newest listing's; "" for neither.
func (d Deps) commune(ctx context.Context, account uuid.UUID) (string, error) {
	addresses, err := d.Accounts.Addresses(ctx, account)
	if err != nil {
		return "", fmt.Errorf("read addresses: %w", err)
	}
	if len(addresses) > 0 {
		return addresses[0].CommuneCode, nil
	}
	ids, err := d.Catalog.OnSale(ctx, account)
	if err != nil || len(ids) == 0 {
		return "", err //nolint:wrapcheck // the caller names the step
	}
	sums, err := d.Catalog.Summaries(ctx, ids[:1])
	if err != nil {
		return "", fmt.Errorf("read the newest listing: %w", err)
	}
	return sums[ids[0]].CommuneCode, nil
}

// copyPhotos saves the listing's photos as the seller's uploads under ids,
// minted before the step, so a retry overwrites the same objects.
func (d Deps) copyPhotos(ctx context.Context, imp domain.Import, images []string, ids []uuid.UUID) (copiedPhotos, error) {
	out := copiedPhotos{Uploads: make([]uuid.UUID, 0, len(images))}
	var unavailable int
	for i, u := range images {
		var (
			b           []byte
			contentType string
			imgErr      error
		)
		for range photoAttempts {
			imgCtx, cancel := context.WithTimeout(ctx, d.CallTimeout)
			b, contentType, imgErr = d.Source.Image(imgCtx, u)
			cancel()
			if !errors.Is(imgErr, chotot.ErrUnavailable) {
				break
			}
		}
		switch {
		case errors.Is(imgErr, chotot.ErrBadImage):
			continue
		case imgErr != nil:
			unavailable++
			continue
		default:
		}
		saveErr := d.Media.SavePhoto(ctx, imp.AccountID, ids[i], b, contentType, mediadomain.OriginLink)
		if errors.Is(saveErr, mediadomain.ErrInvalidUpload) {
			continue
		}
		if saveErr != nil {
			return copiedPhotos{}, fmt.Errorf("save photo %d: %w", i, saveErr)
		}
		out.Uploads = append(out.Uploads, ids[i])
	}
	d.Log.InfoContext(ctx, "chotot import copied", "import", imp.ID, "list_id", imp.ListID, "copied", len(out.Uploads),
		"photos", len(images), "unavailable", unavailable)
	switch {
	case len(out.Uploads) > 0:
		return out, nil
	case unavailable > 0:
		return copiedPhotos{}, fmt.Errorf("copy photos: %d of %d did not come: %w", unavailable, len(images), chotot.ErrUnavailable)
	default:
		return copiedPhotos{State: domain.Unsupported, Reason: domain.ReasonNoPhotos}, nil
	}
}

// PostDraft is UC-LST-04 A1: it posts an import left as a draft with the
// seller's listing, under the import's ID as the key, and lists the import.
// A listed import answers as it is.
func (s ChototImport) PostDraft(ctx restate.Context, in DraftInput) (DraftResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return DraftResult{}, err
	}
	loaded, err := restate.Run(ctx, func(rc restate.RunContext) (DraftResult, error) {
		if _, linked, stepErr := s.Links.Get(rc, in.Account); stepErr != nil || !linked {
			return DraftResult{Refused: domain.ErrNotLinked}, stepErr //nolint:wrapcheck // wrapped below
		}
		imp, found, stepErr := s.Imports.Get(rc, in.Account, in.ListID)
		switch {
		case stepErr != nil:
			return DraftResult{}, stepErr //nolint:wrapcheck // wrapped below
		case found && imp.State == domain.Listed:
			return DraftResult{Import: imp}, nil
		// E8
		case !found || imp.State != domain.Drafted || !imp.Settled(at):
			return DraftResult{Refused: domain.ErrDraftNotFound}, nil
		}
		return DraftResult{Import: imp}, nil
	})
	if err != nil {
		return DraftResult{}, fmt.Errorf("load the draft: %w", err)
	}
	imp := loaded.Import
	if loaded.Refused != nil || imp.State == domain.Listed {
		return loaded, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
	}
	res, err := catalogsvc.PostFrom(ctx, s.Prefix, catalogsvc.PostInput{Seller: in.Account, Key: imp.ID, Listing: in.Listing})
	if err != nil {
		return DraftResult{}, err //nolint:wrapcheck // PostFrom names the call
	}
	if res.Refused != nil {
		return DraftResult{Refused: res.Refused}, nil //nolint:nilerr // A refusal goes back as data.
	}
	imp.Listed(res.ListingID, nil, imp.AdPrice, at)
	if _, err = s.save(ctx, imp, domain.Drafted); err != nil {
		return DraftResult{}, err
	}
	return DraftResult{Import: imp}, nil
}
