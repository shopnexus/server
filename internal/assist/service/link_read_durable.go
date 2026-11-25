package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/assist/domain"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/chotot"
	"github.com/shopnexus/server/internal/provider/llm"
)

// linkRead is the LinkRead service's name without the prefix.
const linkRead = "LinkRead"

type StartLinkInput struct {
	Account uuid.UUID   `json:"account"`
	Key     uuid.UUID   `json:"key,omitzero"`
	Link    domain.Link `json:"link"`
}

// LinkRead reads a seller's listing on another site into a listing draft.
// Start inserts the read, Run reads it in steps, Expire deletes it a day
// later.
type LinkRead struct{ Deps }

func (s LinkRead) ServiceName() string { return s.Prefix + linkRead }

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
	ContactRemoved bool           `json:"contact_removed"`
	Images         []string       `json:"images,omitempty"`
	PhotoCount     int            `json:"photo_count"`
	Failure        domain.Failure `json:"failure,omitempty"`
	Reason         string         `json:"reason,omitempty"`
}

// Start checks the account and inserts the read under the daily cap, each
// a step, then sends Run and the delayed Expire.
func (s LinkRead) Start(ctx restate.Context, in StartLinkInput) (StartResult, error) {
	o, err := s.openRead(ctx, in.Account, in.Key, func(id uuid.UUID, at time.Time) domain.ListingRead {
		return domain.NewLinkRead(id, in.Account, in.Link, at)
	})
	if err != nil {
		return StartResult{}, err
	}
	if o.Answer != nil {
		return *o.Answer, nil
	}
	sendRun(ctx, s.Prefix+linkRead, o.ID, o.ID)
	return StartResult{ReadID: o.ID}, nil
}

// Run is UC-LST-01 A2: a read step that streams the model's text to the
// relay, then the copy step, saving after each. Past the deadline the read
// fails with unavailable.
func (s LinkRead) Run(ctx restate.Context, id uuid.UUID) error {
	run, ok, err := s.loadRun(ctx, id)
	if err != nil || !ok {
		return err
	}
	r, deadline := &run.r, run.deadline
	at, done, err := run.tick()
	if done {
		return err
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
		rep, modelErr := s.writeReply(rc, id, llm.Request{System: linkSystemPrompt, User: linkUserPrompt(ad)}, 1, domain.LinkText)
		if refusedModel(modelErr) {
			s.Log.WarnContext(rc, "the model refused the link read", "read", id, "err", modelErr)
			return readListing{Failure: domain.FailUnavailable, Reason: modelReason(modelErr)}, nil
		}
		if modelErr != nil {
			return readListing{}, modelErr
		}
		d, removed, cleanErr := domain.LinkDraft(rep, ad.Price)
		if cleanErr == nil {
			d.CategoryCode = s.guessCategory(rc, id, d)
		}
		images := ad.Images[:min(len(ad.Images), domain.MaxCopied)]
		// One line per answer, with no word of the seller's: no subject,
		// body, title, description or link.
		s.Log.InfoContext(rc, "link read filled", "read", id, "prompt_version", LinkPromptVersion, "list_id", r.Link().ListID,
			"photos", len(ad.Images), "body_chars", utf8.RuneCountInString(ad.Body), "chotot_category", ad.CategoryName,
			"is_item", cleanErr == nil, "category", d.CategoryCode, "contact_removed", removed, "price", d.Price,
			"ms", time.Since(started).Milliseconds())
		if noItem, ok := errors.AsType[domain.NoItemError](cleanErr); ok {
			return readListing{Failure: domain.FailNoItem, Reason: noItem.Reason}, nil
		}
		if cleanErr != nil {
			return readListing{}, fmt.Errorf("clean the answer: %w", cleanErr)
		}
		return readListing{Draft: d, ContactRemoved: removed, Images: images, PhotoCount: len(ad.Images)}, nil
	}, restate.WithMaxRetryDuration(deadline.Sub(at)), restate.WithMaxRetryAttempts(modelAttempts))
	if err != nil {
		return run.fail(at, domain.FailUnavailable, modelReason(err))
	}
	if read.Failure != "" {
		return run.fail(at, read.Failure, read.Reason)
	}
	if err = r.SetLinkPhotos(read.PhotoCount); err != nil {
		return fmt.Errorf("set photo count: %w", err)
	}
	if gone, saveErr := run.save(at); saveErr != nil || gone {
		return saveErr
	}

	if at, done, err = run.tick(); done {
		return err
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
		return run.fail(at, domain.FailUnavailable, "")
	}
	if copied.Failure != "" {
		return run.fail(at, copied.Failure, "")
	}
	d := read.Draft
	d.Photos = copied.Uploads
	photos := make([]domain.Photo, len(copied.Uploads))
	for i, u := range copied.Uploads {
		photos[i] = domain.Photo{UploadID: u}
	}
	if err = r.SetPhotos(photos); err != nil {
		return fmt.Errorf("set photos: %w", err)
	}
	if err = r.SetDraft(d, read.ContactRemoved); err != nil {
		return fmt.Errorf("set draft: %w", err)
	}
	return run.finish(at)
}

// Expire deletes a read a day after it started. Copies not posted expire
// with their uploads.
func (s LinkRead) Expire(ctx restate.Context, id uuid.UUID) error { return s.expireRead(ctx, id) }
