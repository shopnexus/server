package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/assist/domain"
	"github.com/shopnexus/server2/internal/assist/repository"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/chotot"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// linkRead is the LinkRead service's name without the prefix.
const linkRead = "LinkRead"

type StartLinkInput struct {
	Account uuid.UUID   `json:"account"`
	Link    domain.Link `json:"link"`
}

// LinkRead reads a seller's listing on another site into a listing draft.
// Start inserts the read, Run reads it in steps, Expire deletes it a day
// later.
type LinkRead struct{ Deps }

func (s LinkRead) ServiceName() string { return s.Prefix + linkRead }

// loadedLinkRead is what a load step found.
type loadedLinkRead struct {
	Read  domain.LinkRead `json:"read"`
	Found bool            `json:"found"`
}

// savedLinkRead is the read a save step wrote, or Gone when the row went
// meanwhile.
type savedLinkRead struct {
	Read domain.LinkRead `json:"read"`
	Gone bool            `json:"gone"`
}

// photoAttempts is how many times the copy step asks the CDN for one photo
// before it skips it.
const photoAttempts = 2

// copiedPhotos is what the copy step saved, in the listing's order, or the
// failure when it saved none.
type copiedPhotos struct {
	Uploads []uuid.UUID    `json:"uploads"`
	Failure domain.Failure `json:"failure,omitempty"`
}

// readListing is what the read step made of the listing. The listing's
// subject and body are not in it: they stay inside the step.
type readListing struct {
	Draft          domain.Draft   `json:"draft"`
	Range          *domain.Range  `json:"range,omitempty"`
	ContactRemoved bool           `json:"contact_removed"`
	Images         []string       `json:"images,omitempty"`
	PhotoCount     int            `json:"photo_count"`
	Failure        domain.Failure `json:"failure,omitempty"`
}

// Start checks the account and inserts the read under the daily cap, each
// a step, then sends Run and the delayed Expire.
func (s LinkRead) Start(ctx restate.Context, in StartLinkInput) (StartResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return StartResult{}, err
	}
	status, err := restate.Run(ctx, func(rc restate.RunContext) (accountdomain.Status, error) {
		return s.Accounts.Status(rc, in.Account)
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return StartResult{Refused: r}, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
	}
	// A read exists to post a listing, and posting needs a phone.
	hasPhone, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		return s.Accounts.HasPhone(rc, in.Account)
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("read the account's phone: %w", err)
	}
	if !hasPhone {
		return StartResult{Refused: accountdomain.ErrPhoneRequired}, nil
	}
	id := restatex.NewID(ctx, at)
	read := domain.NewLinkRead(id, in.Account, in.Link, at)
	inserted, err := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
		return restatex.AsRefusal(s.LinkReads.Insert(rc, read))
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("insert read: %w", err)
	}
	if inserted.Refusal != nil {
		return StartResult{Refused: inserted.Refusal}, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
	}
	restate.ServiceSend(ctx, s.Prefix+linkRead, "Expire").
		Send(id, restate.WithDelay(domain.KeepFor), restate.WithIdempotencyKey(id.String()))
	restate.ServiceSend(ctx, s.Prefix+linkRead, "Run").Send(id, restate.WithIdempotencyKey(id.String()))
	return StartResult{ReadID: id}, nil
}

// Run reads the listing in three steps, read, copy and estimate, and saves
// the read after each, which notifies its account. Before each step it
// reads the journaled clock: past the deadline the read fails with
// unavailable, and the read and copy steps retry only until the deadline.
// The estimate step never fails a read: without it the model's range
// stands.
func (s LinkRead) Run(ctx restate.Context, id uuid.UUID) error {
	var err error
	loaded, err := restate.Run(ctx, func(rc restate.RunContext) (loadedLinkRead, error) {
		r, stepErr := s.LinkReads.Get(rc, id)
		if errors.Is(stepErr, domain.ErrLinkReadNotFound) {
			return loadedLinkRead{}, nil
		}
		if stepErr != nil {
			return loadedLinkRead{}, fmt.Errorf("get read: %w", stepErr)
		}
		return loadedLinkRead{Read: r, Found: true}, nil
	})
	if err != nil {
		return fmt.Errorf("load read: %w", err)
	}
	if !loaded.Found || loaded.Read.Status() != domain.StatusReading {
		return nil
	}
	r := loaded.Read
	deadline := r.CreatedAt().Add(domain.Deadline)

	// save writes r under a new write ID and keeps the saved copy, so the
	// next save carries its write ID.
	save := func(at time.Time) (gone bool, err error) {
		w := restatex.NewID(ctx, at)
		out, saveErr := restate.Run(ctx, func(rc restate.RunContext) (savedLinkRead, error) {
			c := r // Save sets the write ID on its copy; r outside stays as journaled
			lost, stepErr := restatex.Outcome(s.LinkReads.Save(rc, &c, w), repository.ErrConflict)
			if stepErr != nil {
				return savedLinkRead{}, fmt.Errorf("save read: %w", stepErr)
			}
			return savedLinkRead{Read: c, Gone: lost}, nil
		})
		if saveErr != nil {
			return false, fmt.Errorf("save read: %w", saveErr)
		}
		r = out.Read
		return out.Gone, nil
	}
	fail := func(at time.Time, f domain.Failure) error {
		if failErr := r.Fail(f); failErr != nil {
			return fmt.Errorf("fail read: %w", failErr)
		}
		_, saveErr := save(at)
		return saveErr
	}

	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return err
	}
	if !at.Before(deadline) {
		return fail(at, domain.FailUnavailable)
	}
	read, err := restate.Run(ctx, func(rc restate.RunContext) (readListing, error) {
		started := time.Now()
		adCtx, cancelAd := context.WithTimeout(rc, s.CallTimeout)
		ad, adErr := s.Source.Ad(adCtx, r.Link().ListID)
		cancelAd()
		switch {
		case errors.Is(adErr, chotot.ErrNotFound):
			return readListing{Failure: domain.FailListingNotFound}, nil
		case errors.Is(adErr, chotot.ErrFormat):
			s.Log.ErrorContext(rc, "chotot format: the gateway's answer changed", "read", id, "list_id", r.Link().ListID, "err", adErr)
			return readListing{Failure: domain.FailUnavailable}, nil
		case adErr != nil:
			return readListing{}, fmt.Errorf("read the listing: %w", adErr)
		}
		cats, catErr := s.Catalog.Categories(rc)
		if catErr != nil {
			return readListing{}, fmt.Errorf("read categories: %w", catErr)
		}
		var answer domain.LinkAnswer
		modelCtx, cancelModel := context.WithTimeout(rc, s.CallTimeout)
		modelErr := s.Model.Complete(modelCtx, llm.Request{
			System: linkSystemPrompt(cats), User: linkUserPrompt(ad),
			SchemaName: domain.LinkAnswerSchemaName, Schema: domain.LinkAnswerSchema,
		}, &answer)
		cancelModel()
		if errors.Is(modelErr, llm.ErrRefused) {
			s.Log.WarnContext(rc, "the model refused the link read", "read", id, "err", modelErr)
			return readListing{Failure: domain.FailUnavailable}, nil
		}
		if modelErr != nil {
			return readListing{}, fmt.Errorf("ask the model: %w", modelErr)
		}
		d, rng, removed, cleanErr := domain.CleanLink(answer, ad.Price, cats)
		images := ad.Images[:min(len(ad.Images), domain.MaxCopied)]
		// One line per answer, with no word of the seller's: no subject,
		// body, title, description or link.
		s.Log.InfoContext(rc, "link read filled", "read", id, "prompt_version", LinkPromptVersion, "list_id", r.Link().ListID,
			"photos", len(ad.Images), "body_chars", utf8.RuneCountInString(ad.Body), "chotot_category", ad.CategoryName,
			"is_item", answer.IsItem, "category_asked", answer.CategoryCode, "category_kept", d.CategoryCode,
			"condition", d.Condition, "specs", len(d.Specs), "contact_removed", removed, "price", d.Price, "range", rng,
			"ms", time.Since(started).Milliseconds())
		if errors.Is(cleanErr, domain.ErrNoItem) {
			return readListing{Failure: domain.FailNoItem}, nil
		}
		if cleanErr != nil {
			return readListing{}, fmt.Errorf("clean the answer: %w", cleanErr)
		}
		return readListing{Draft: d, Range: rng, ContactRemoved: removed, Images: images, PhotoCount: len(ad.Images)}, nil
	}, restate.WithMaxRetryDuration(deadline.Sub(at)), restate.WithMaxRetryAttempts(modelAttempts))
	if err != nil {
		return fail(at, domain.FailUnavailable)
	}
	if read.Failure != "" {
		return fail(at, read.Failure)
	}
	if err = r.SetPhotoCount(read.PhotoCount); err != nil {
		return fmt.Errorf("set photo count: %w", err)
	}
	if gone, saveErr := save(at); saveErr != nil || gone {
		return saveErr
	}

	if at, err = restatex.Now(ctx, s.Now); err != nil {
		return err
	}
	if !at.Before(deadline) {
		return fail(at, domain.FailUnavailable)
	}
	ids := make([]uuid.UUID, len(read.Images))
	for i := range ids {
		ids[i] = restatex.NewID(ctx, at)
	}
	copied, err := restate.Run(ctx, func(rc restate.RunContext) (copiedPhotos, error) {
		out := copiedPhotos{Uploads: make([]uuid.UUID, 0, len(read.Images))}
		var unavailable, offCDN, refused int
		for i, u := range read.Images {
			// A photo the CDN does not answer gets photoAttempts tries, then
			// is skipped, so one broken photo does not hold the listing back.
			var (
				b           []byte
				contentType string
				imgErr      error
			)
			for range photoAttempts {
				imgCtx, cancel := context.WithTimeout(rc, s.CallTimeout)
				b, contentType, imgErr = s.Source.Image(imgCtx, u)
				cancel()
				if !errors.Is(imgErr, chotot.ErrUnavailable) {
					break
				}
			}
			switch {
			case errors.Is(imgErr, chotot.ErrOffCDN):
				offCDN++
				continue
			case errors.Is(imgErr, chotot.ErrBadImage):
				refused++
				s.Log.WarnContext(rc, "a photo of the listing was skipped", "read", id, "photo", i, "err", imgErr)
				continue
			case imgErr != nil:
				unavailable++
				s.Log.WarnContext(rc, "a photo of the listing did not come", "read", id, "photo", i, "err", imgErr)
				continue
			default:
			}
			// The ID was minted before the step, so a retry overwrites the
			// same object and inserts no second upload.
			saveErr := s.Media.SavePhoto(rc, r.AccountID(), ids[i], b, contentType, mediadomain.OriginLink)
			if errors.Is(saveErr, mediadomain.ErrInvalidUpload) {
				refused++
				s.Log.WarnContext(rc, "a photo of the listing was refused", "read", id, "photo", i, "err", saveErr)
				continue
			}
			if saveErr != nil {
				return copiedPhotos{}, fmt.Errorf("save photo %d: %w", i, saveErr)
			}
			out.Uploads = append(out.Uploads, ids[i])
		}
		s.Log.InfoContext(rc, "link read copied", "read", id, "list_id", r.Link().ListID, "copied", len(out.Uploads),
			"refused", refused, "unavailable", unavailable, "off_cdn", offCDN)
		switch {
		case len(out.Uploads) > 0:
			return out, nil
		case unavailable > 0:
			// Nothing came and the CDN did not answer: the step tries again.
			return copiedPhotos{}, fmt.Errorf("copy photos: %d of %d did not come: %w", unavailable, len(read.Images), chotot.ErrUnavailable)
		case offCDN > 0:
			s.Log.ErrorContext(rc, "chotot format: the listing's photos are off CHOTOT_CDN_URL", "read", id,
				"list_id", r.Link().ListID, "photos", offCDN)
			return copiedPhotos{Failure: domain.FailUnavailable}, nil
		default:
			return copiedPhotos{Failure: domain.FailListingNotFound}, nil
		}
	}, restate.WithMaxRetryDuration(deadline.Sub(at)), restate.WithMaxRetryAttempts(modelAttempts))
	if err != nil {
		return fail(at, domain.FailUnavailable)
	}
	if copied.Failure != "" {
		return fail(at, copied.Failure)
	}
	d := read.Draft
	d.Photos = copied.Uploads
	if err = r.SetDraft(d, read.ContactRemoved); err != nil {
		return fmt.Errorf("set draft: %w", err)
	}
	if gone, saveErr := save(at); saveErr != nil || gone {
		return saveErr
	}

	if at, err = restatex.Now(ctx, s.Now); err != nil {
		return err
	}
	est := s.marketEstimate(ctx, id, r.AccountID(), d, at, deadline)
	if est == nil {
		est = domain.ModelEstimate(read.Range)
	}
	if err = r.Finish(est); err != nil {
		return fmt.Errorf("finish read: %w", err)
	}
	_, err = save(at)
	return err
}

// Expire deletes a read a day after it started. Copies not posted expire
// with their uploads.
func (s LinkRead) Expire(ctx restate.Context, id uuid.UUID) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.LinkReads.Delete(rc, id) }); err != nil {
		return fmt.Errorf("delete read: %w", err)
	}
	return nil
}
