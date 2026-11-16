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
	"github.com/shopnexus/server2/internal/provider/llm"
)

// photoRead is the PhotoRead service's name without the prefix.
const photoRead = "PhotoRead"

type StartPhotoInput struct {
	Account uuid.UUID `json:"account"`
	Photo   uuid.UUID `json:"photo"`
}

// PhotoRead reads one photo of a seller's into a listing draft. Start
// inserts the read under the photo cap and checks the photo, Run fills the
// draft and estimates the price, Expire deletes the read a day later. The
// read never holds the photo: the listing posts it.
type PhotoRead struct{ Deps }

func (s PhotoRead) ServiceName() string { return s.Prefix + photoRead }

// filledPhoto is what the fill step made of the photo, or the failure.
type filledPhoto struct {
	Draft   domain.Draft   `json:"draft"`
	Range   *domain.Range  `json:"range,omitempty"`
	Failure domain.Failure `json:"failure,omitempty"`
}

// Start checks the account, inserts the read under the daily cap and
// checks the photo, each a step, then sends Run and the delayed Expire. A
// photo the check refuses deletes the read again.
func (s PhotoRead) Start(ctx restate.Context, in StartPhotoInput) (StartResult, error) {
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
	read := domain.NewPhotoRead(id, in.Account, in.Photo, at)
	inserted, err := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
		return restatex.AsRefusal(s.Reads.Insert(rc, read, ""))
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("insert read: %w", err)
	}
	if inserted.Refusal != nil {
		return StartResult{Refused: inserted.Refusal}, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
	}
	// The photo must be the caller's, waiting, before its expiry and in the
	// store as declared: what a search by photo takes.
	checked, err := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
		_, queryErr := s.Media.QueryUpload(rc, in.Account, in.Photo)
		return restatex.AsRefusal(queryErr)
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("check photo: %w", err)
	}
	if checked.Refusal != nil {
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Reads.Delete(rc, id) }); err != nil {
			return StartResult{}, fmt.Errorf("undo read: %w", err)
		}
		return StartResult{Refused: checked.Refusal}, nil
	}
	restate.ServiceSend(ctx, s.Prefix+photoRead, "Expire").
		Send(id, restate.WithDelay(domain.KeepFor), restate.WithIdempotencyKey(id.String()))
	restate.ServiceSend(ctx, s.Prefix+photoRead, "Run").Send(id, restate.WithIdempotencyKey(id.String()))
	return StartResult{ReadID: id}, nil
}

// Run is UC-LST-01, step 2: it reads the photo in two steps, fill and
// estimate, and saves the read after each, which notifies its account. Past
// the deadline the read fails with unavailable, and the fill step retries
// only until the deadline. The estimate step never fails a read: without it
// the model's range stands.
func (s PhotoRead) Run(ctx restate.Context, id uuid.UUID) error {
	var err error
	loaded, err := restate.Run(ctx, func(rc restate.RunContext) (loadedRead, error) {
		r, stepErr := s.Reads.Get(rc, id)
		if errors.Is(stepErr, domain.ErrNotFound) {
			return loadedRead{}, nil
		}
		if stepErr != nil {
			return loadedRead{}, fmt.Errorf("get read: %w", stepErr)
		}
		return loadedRead{Read: r, Found: true}, nil
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
		out, saveErr := restate.Run(ctx, func(rc restate.RunContext) (savedRead, error) {
			c := r // Save sets the write ID on its copy; r outside stays as journaled
			lost, stepErr := restatex.Outcome(s.Reads.Save(rc, &c, w), repository.ErrConflict)
			if stepErr != nil {
				return savedRead{}, fmt.Errorf("save read: %w", stepErr)
			}
			return savedRead{Read: c, Gone: lost}, nil
		})
		if saveErr != nil {
			return false, fmt.Errorf("save read: %w", saveErr)
		}
		r = out.Read
		return out.Gone, nil
	}
	fail := func(at time.Time, f domain.Failure) error {
		// A1: the read fails and the seller fills the form by hand.
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
	fill, err := restate.Run(ctx, func(rc restate.RunContext) (filledPhoto, error) {
		started := time.Now()
		img, imgErr := s.Media.QueryImage(rc, r.AccountID(), r.Media())
		if errors.Is(imgErr, mediadomain.ErrInvalidUpload) {
			// Posted in a listing, expired or gone since Start.
			return filledPhoto{Failure: domain.FailUnreadable}, nil
		}
		if imgErr != nil {
			return filledPhoto{}, fmt.Errorf("read photo: %w", imgErr)
		}
		cats, catErr := s.Catalog.Categories(rc)
		if catErr != nil {
			return filledPhoto{}, fmt.Errorf("read categories: %w", catErr)
		}
		var answer domain.PhotoAnswer
		modelCtx, cancelModel := context.WithTimeout(rc, s.CallTimeout)
		modelErr := s.Model.Complete(modelCtx, llm.Request{
			System: photoSystemPrompt(cats), User: photoUserPrompt,
			SchemaName: domain.PhotoAnswerSchemaName, Schema: domain.PhotoAnswerSchema, Images: [][]byte{img},
		}, &answer)
		cancelModel()
		if errors.Is(modelErr, llm.ErrRefused) {
			s.Log.WarnContext(rc, "the model refused the photo read", "read", id, "err", modelErr)
			return filledPhoto{Failure: domain.FailUnavailable}, nil
		}
		if modelErr != nil {
			return filledPhoto{}, fmt.Errorf("ask the model: %w", modelErr)
		}
		d, rng, cleanErr := domain.CleanPhoto(answer, r.Media(), cats)
		// One line per answer, with no word of the draft's: no title or
		// description.
		s.Log.InfoContext(rc, "photo read filled", "read", id, "prompt_version", PhotoPromptVersion, "is_item", answer.IsItem,
			"category_asked", answer.CategoryCode, "category_kept", d.CategoryCode, "condition", d.Condition,
			"specs", len(d.Specs), "description_chars", utf8.RuneCountInString(d.Description), "range", rng,
			"ms", time.Since(started).Milliseconds())
		if errors.Is(cleanErr, domain.ErrNoItem) {
			return filledPhoto{Failure: domain.FailNoItem}, nil
		}
		if cleanErr != nil {
			return filledPhoto{}, fmt.Errorf("clean the answer: %w", cleanErr)
		}
		return filledPhoto{Draft: d, Range: rng}, nil
	}, restate.WithMaxRetryDuration(deadline.Sub(at)), restate.WithMaxRetryAttempts(modelAttempts))
	if err != nil {
		return fail(at, domain.FailUnavailable)
	}
	if fill.Failure != "" {
		return fail(at, fill.Failure)
	}
	if err = r.SetPhotos([]domain.Photo{{UploadID: r.Media()}}); err != nil {
		return fmt.Errorf("set photos: %w", err)
	}
	if err = r.SetDraft(fill.Draft, false); err != nil {
		return fmt.Errorf("set draft: %w", err)
	}
	if gone, saveErr := save(at); saveErr != nil || gone {
		return saveErr
	}

	if at, err = restatex.Now(ctx, s.Now); err != nil {
		return err
	}
	est := s.marketEstimate(ctx, id, r.AccountID(), fill.Draft, at, deadline)
	if est == nil {
		est = domain.ModelEstimate(fill.Range)
	}
	if err = r.Finish(est); err != nil {
		return fmt.Errorf("finish read: %w", err)
	}
	_, err = save(at)
	return err
}

// Expire deletes a read a day after it started. The photo is the seller's
// own upload and expires, or is posted, on its own.
func (s PhotoRead) Expire(ctx restate.Context, id uuid.UUID) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Reads.Delete(rc, id) }); err != nil {
		return fmt.Errorf("delete read: %w", err)
	}
	return nil
}
