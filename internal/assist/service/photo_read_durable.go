package service

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/assist/domain"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/llm"
)

// photoRead is the PhotoRead service's name without the prefix.
const photoRead = "PhotoRead"

type StartPhotoInput struct {
	Account uuid.UUID `json:"account"`
	Key     uuid.UUID `json:"key,omitzero"`
	Photo   uuid.UUID `json:"photo"`
}

// PhotoRead reads one photo of a seller's into a listing draft. Start
// inserts the read under the photo cap and checks the photo, Run has the
// model write the title and description, Expire deletes the read a day
// later. The read never holds the photo: the listing posts it.
type PhotoRead struct{ Deps }

func (s PhotoRead) ServiceName() string { return s.Prefix + photoRead }

// filledPhoto is what the fill step made of the photo, or the failure.
type filledPhoto struct {
	Draft   domain.Draft   `json:"draft"`
	Failure domain.Failure `json:"failure,omitempty"`
	Reason  string         `json:"reason,omitempty"`
}

// Start checks the account, inserts the read under the daily cap and
// checks the photo, each a step, then sends Run and the delayed Expire. A
// photo the check refuses deletes the read again.
func (s PhotoRead) Start(ctx restate.Context, in StartPhotoInput) (StartResult, error) {
	o, err := s.openRead(ctx, in.Account, in.Key, func(id uuid.UUID, at time.Time) domain.ListingRead {
		return domain.NewPhotoRead(id, in.Account, in.Photo, at)
	})
	if err != nil {
		return StartResult{}, err
	}
	if o.Answer != nil {
		return *o.Answer, nil
	}
	id := o.ID
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
		if err = s.deleteRead(ctx, id); err != nil {
			return StartResult{}, fmt.Errorf("undo read: %w", err)
		}
		return StartResult{Refused: checked.Refusal}, nil
	}
	sendRun(ctx, s.Prefix+photoRead, id, id)
	return StartResult{ReadID: id}, nil
}

// Run is UC-LST-01, step 2: one fill step streams the model's text to the
// relay, then the read saves done or failed. Past the deadline it fails
// with unavailable.
func (s PhotoRead) Run(ctx restate.Context, id uuid.UUID) error {
	run, ok, err := s.loadRun(ctx, id)
	if err != nil || !ok {
		return err
	}
	r, deadline := &run.r, run.deadline
	at, done, err := run.tick()
	if done {
		return err
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
		rep, modelErr := s.writeReply(rc, id, llm.Request{System: photoSystemPrompt, User: photoUserPrompt, Images: [][]byte{img}}, mediaTries,
			func(so string) domain.Text { return domain.PartialText(so, domain.MaxPhotoDescription) })
		if refusedModel(modelErr) {
			s.Log.WarnContext(rc, "the model refused the photo read", "read", id, "err", modelErr)
			return filledPhoto{Failure: domain.FailUnavailable, Reason: modelReason(modelErr)}, nil
		}
		if modelErr != nil {
			return filledPhoto{}, modelErr
		}
		d, cleanErr := domain.PhotoDraft(rep, r.Media())
		if cleanErr == nil {
			d.CategoryCode = s.guessCategory(rc, id, d)
		}
		// One line per answer, with no word of the draft's: no title or
		// description.
		s.Log.InfoContext(rc, "photo read filled", "read", id, "prompt_version", PhotoPromptVersion,
			"is_item", cleanErr == nil, "category", d.CategoryCode,
			"description_chars", utf8.RuneCountInString(d.Description), "ms", time.Since(started).Milliseconds())
		if noItem, ok := errors.AsType[domain.NoItemError](cleanErr); ok {
			return filledPhoto{Failure: domain.FailNoItem, Reason: noItem.Reason}, nil
		}
		if cleanErr != nil {
			return filledPhoto{}, fmt.Errorf("clean the answer: %w", cleanErr)
		}
		return filledPhoto{Draft: d}, nil
	}, restate.WithMaxRetryDuration(deadline.Sub(at)), restate.WithMaxRetryAttempts(modelAttempts))
	if err != nil {
		return run.fail(at, domain.FailUnavailable, modelReason(err))
	}
	if fill.Failure != "" {
		return run.fail(at, fill.Failure, fill.Reason)
	}
	if err = r.SetPhotos([]domain.Photo{{UploadID: r.Media()}}); err != nil {
		return fmt.Errorf("set photos: %w", err)
	}
	if err = r.SetDraft(fill.Draft, false); err != nil {
		return fmt.Errorf("set draft: %w", err)
	}
	return run.finish(at)
}

// Expire deletes a read a day after it started. The photo is the seller's
// own upload and expires, or is posted, on its own.
func (s PhotoRead) Expire(ctx restate.Context, id uuid.UUID) error { return s.expireRead(ctx, id) }
