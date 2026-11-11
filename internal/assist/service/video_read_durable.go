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
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/objectstore"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/frames"
	"github.com/shopnexus/server2/internal/provider/llm"
	"github.com/shopnexus/server2/internal/provider/stt"
)

// videoRead is the VideoRead service's name without the prefix.
const videoRead = "VideoRead"

// maxFrames is the most frames ai's /v1/video/frames answers; the IDs of
// that many uploads are minted before the frames step.
const maxFrames = 8

type StartInput struct {
	Account uuid.UUID `json:"account"`
	Video   uuid.UUID `json:"video"`
}

// modelAttempts bounds each model step: SDK v1.1.0 retries a step every
// 2 s at most, so a step bounded only by the deadline would transcribe the
// video and ask the model again some 40 times during an outage.
const modelAttempts = 3

// ExpireInput names the read Expire deletes and the video it holds, so a
// read whose row is already gone still has its video consumed.
type ExpireInput struct {
	Read  uuid.UUID `json:"read"`
	Video uuid.UUID `json:"video"`
}

// StartResult names the read started, or carries the refusal.
type StartResult struct {
	ReadID  uuid.UUID     `json:"read_id"`
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r StartResult) Refusal() *apperr.Error { return r.Refused }

// VideoRead reads a seller's video into a listing draft. Start inserts the
// read and holds the video, Run reads it in steps, Expire deletes it a day
// later.
type VideoRead struct{ Deps }

func (s VideoRead) ServiceName() string { return s.Prefix + videoRead }

// loadedRead is what a load step found.
type loadedRead struct {
	Read  domain.VideoRead `json:"read"`
	Found bool             `json:"found"`
}

// savedRead is the read a save step wrote, with its new write ID, or Gone
// when the row went meanwhile: Expire or the account's deletion took it.
type savedRead struct {
	Read domain.VideoRead `json:"read"`
	Gone bool             `json:"gone"`
}

// cutFrames is what the frames step kept: the frames saved as uploads and
// the sharpest one, or the failure ai's answer means.
type cutFrames struct {
	Frames   []domain.Frame `json:"frames,omitempty"`
	Sharpest int            `json:"sharpest"`
	Failure  domain.Failure `json:"failure,omitempty"`
}

// filled is what the fill step made of the frames and the speech. The
// transcript is not in it: it stays inside the step.
type filled struct {
	Draft   domain.Draft   `json:"draft"`
	Range   *domain.Range  `json:"range,omitempty"`
	Failure domain.Failure `json:"failure,omitempty"`
}

// Start checks the account, inserts the read under the daily cap and
// holds the video, each a step, then sends Run and the delayed Expire. A
// video the hold refuses deletes the read again.
func (s VideoRead) Start(ctx restate.Context, in StartInput) (StartResult, error) {
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
	// A read exists to post a listing, and posting needs a phone; asking
	// for it here keeps accounts made of an email alone off the model.
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
	read := domain.NewOldVideoRead(id, in.Account, in.Video, at)
	inserted, err := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
		return restatex.AsRefusal(s.VideoReads.Insert(rc, read))
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("insert read: %w", err)
	}
	if inserted.Refusal != nil {
		return StartResult{Refused: inserted.Refusal}, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
	}
	held, err := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
		holdErr := s.Media.HoldVideo(rc, in.Account, id, in.Video)
		if errors.Is(holdErr, apperr.ErrTryAgain) {
			// Another start's hold saved first; the retry sees its holder
			// and refuses with invalid_upload.
			return restatex.Refused{}, fmt.Errorf("hold video: %w", holdErr)
		}
		return restatex.AsRefusal(holdErr)
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("hold video: %w", err)
	}
	if held.Refusal != nil {
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.VideoReads.Delete(rc, id) }); err != nil {
			return StartResult{}, fmt.Errorf("undo read: %w", err)
		}
		return StartResult{Refused: held.Refusal}, nil
	}
	restate.ServiceSend(ctx, s.Prefix+videoRead, "Expire").
		Send(ExpireInput{Read: id, Video: in.Video}, restate.WithDelay(domain.KeepFor), restate.WithIdempotencyKey(id.String()))
	restate.ServiceSend(ctx, s.Prefix+videoRead, "Run").Send(id, restate.WithIdempotencyKey(id.String()))
	return StartResult{ReadID: id}, nil
}

// Run reads the video in three steps, frames, fill and estimate, and saves
// the read after each, which notifies its account. Every path that ends
// the read, done or failed, consumes the video. Before each step it reads
// the journaled clock: past the deadline the read fails with unavailable,
// and the frames and fill steps retry only until the deadline, so one that
// keeps failing ends the same way. The estimate step never fails a read:
// without it the model's range stands.
func (s VideoRead) Run(ctx restate.Context, id uuid.UUID) error {
	// restate.Run answers a TerminalError; err holds the other calls' errors too.
	var err error
	loaded, err := restate.Run(ctx, func(rc restate.RunContext) (loadedRead, error) {
		r, stepErr := s.VideoReads.Get(rc, id)
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
			lost, stepErr := restatex.Outcome(s.VideoReads.Save(rc, &c, w), repository.ErrConflict)
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
	consume := func() error {
		if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Media.Consume(rc, r.VideoID()) }); err != nil {
			return fmt.Errorf("consume video: %w", err)
		}
		return nil
	}
	// fail consumes the video before it saves the failure, so a read that
	// shows it ended has no video left.
	fail := func(at time.Time, f domain.Failure) error {
		if failErr := r.Fail(f); failErr != nil {
			return fmt.Errorf("fail read: %w", failErr)
		}
		if consumeErr := consume(); consumeErr != nil {
			return consumeErr
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
	ids := make([]uuid.UUID, maxFrames)
	for i := range ids {
		ids[i] = restatex.NewID(ctx, at)
	}
	cut, err := restate.Run(ctx, func(rc restate.RunContext) (cutFrames, error) {
		video, size, openErr := s.Media.OpenVideo(rc, r.VideoID())
		if errors.Is(openErr, objectstore.ErrNotFound) {
			return cutFrames{Failure: domain.FailUnreadable}, nil
		}
		if openErr != nil {
			return cutFrames{}, fmt.Errorf("open video: %w", openErr)
		}
		defer func() { _ = video.Close() }()
		callCtx, cancel := context.WithTimeout(rc, s.CallTimeout)
		defer cancel()
		res, cutErr := s.Frames.Extract(callCtx, video, size)
		switch {
		case errors.Is(cutErr, frames.ErrUnreadable):
			return cutFrames{Failure: domain.FailUnreadable}, nil
		case errors.Is(cutErr, frames.ErrRefused):
			// ai refused the request itself, not the video.
			s.Log.WarnContext(rc, "ai refused the frames request", "read", id, "err", cutErr)
			return cutFrames{Failure: domain.FailUnavailable}, nil
		case errors.Is(cutErr, frames.ErrTooShort):
			return cutFrames{Failure: domain.FailTooShort}, nil
		case errors.Is(cutErr, frames.ErrTooLong):
			return cutFrames{Failure: domain.FailTooLong}, nil
		case cutErr != nil:
			return cutFrames{}, fmt.Errorf("cut frames: %w", cutErr)
		}
		if len(res.Frames) == 0 {
			return cutFrames{Failure: domain.FailUnreadable}, nil
		}
		res.Frames = res.Frames[:min(len(res.Frames), maxFrames)]
		out := cutFrames{Frames: make([]domain.Frame, len(res.Frames))}
		for i, f := range res.Frames {
			// The ID was minted before the step, so a retry overwrites the
			// same object and inserts no second upload.
			if saveErr := s.Media.SavePhoto(rc, r.AccountID(), ids[i], f.JPEG, "image/jpeg", mediadomain.OriginFrame); saveErr != nil {
				return cutFrames{}, fmt.Errorf("save frame %d: %w", i, saveErr)
			}
			out.Frames[i] = domain.Frame{UploadID: ids[i], AtSeconds: f.AtSeconds}
			if f.Sharpness > res.Frames[out.Sharpest].Sharpness {
				out.Sharpest = i
			}
		}
		return out, nil
	}, restate.WithMaxRetryDuration(deadline.Sub(at)), restate.WithMaxRetryAttempts(modelAttempts))
	if err != nil {
		return fail(at, domain.FailUnavailable)
	}
	if cut.Failure != "" {
		return fail(at, cut.Failure)
	}
	if err = r.SetFrames(cut.Frames); err != nil {
		return fmt.Errorf("set frames: %w", err)
	}
	if gone, saveErr := save(at); saveErr != nil || gone {
		return errors.Join(saveErr, consume())
	}

	if at, err = restatex.Now(ctx, s.Now); err != nil {
		return err
	}
	if !at.Before(deadline) {
		return fail(at, domain.FailUnavailable)
	}
	fill, err := restate.Run(ctx, func(rc restate.RunContext) (filled, error) {
		video, size, openErr := s.Media.OpenVideo(rc, r.VideoID())
		if errors.Is(openErr, objectstore.ErrNotFound) {
			return filled{Failure: domain.FailUnreadable}, nil
		}
		if openErr != nil {
			return filled{}, fmt.Errorf("open video: %w", openErr)
		}
		defer func() { _ = video.Close() }()
		started := time.Now()
		sttCtx, cancelSTT := context.WithTimeout(rc, s.CallTimeout)
		transcript, sttErr := s.STT.Transcribe(sttCtx, video, size)
		cancelSTT()
		switch {
		case errors.Is(sttErr, stt.ErrNoAudio):
			transcript = ""
		case errors.Is(sttErr, stt.ErrRefused):
			// The frames step read the video, so a refusal is the gateway's
			// (a key, a budget, a rewritten error code): read without speech.
			s.Log.WarnContext(rc, "speech to text refused; the read goes on without speech", "read", id, "err", sttErr)
			transcript = ""
		case sttErr != nil:
			return filled{}, fmt.Errorf("transcribe: %w", sttErr)
		default:
		}
		images := make([][]byte, len(r.Frames()))
		for i, f := range r.Frames() {
			img, imgErr := s.Media.QueryImage(rc, r.AccountID(), f.UploadID)
			if imgErr != nil {
				return filled{}, fmt.Errorf("read frame %d: %w", i, imgErr)
			}
			images[i] = img
		}
		cats, catErr := s.Catalog.Categories(rc)
		if catErr != nil {
			return filled{}, fmt.Errorf("read categories: %w", catErr)
		}
		var answer domain.Answer
		modelCtx, cancelModel := context.WithTimeout(rc, s.CallTimeout)
		modelErr := s.Model.Complete(modelCtx, llm.Request{
			System: systemPrompt(cats), User: userPrompt(transcript, len(images)),
			SchemaName: domain.AnswerSchemaName, Schema: domain.AnswerSchema, Images: images,
		}, &answer)
		cancelModel()
		if errors.Is(modelErr, llm.ErrRefused) {
			s.Log.WarnContext(rc, "the model refused the video read", "read", id, "err", modelErr)
			return filled{Failure: domain.FailUnavailable}, nil
		}
		if modelErr != nil {
			return filled{}, fmt.Errorf("ask the model: %w", modelErr)
		}
		d, rng, cleanErr := domain.Clean(answer, r.Frames(), cut.Sharpest, cats)
		// One line per answer, with no word of the seller's: no transcript,
		// title or description.
		s.Log.InfoContext(rc, "video read filled", "read", id, "prompt_version", PromptVersion,
			"frames", len(images), "transcript_chars", utf8.RuneCountInString(transcript), "is_item", answer.IsItem,
			"category_asked", answer.CategoryCode, "category_kept", d.CategoryCode, "condition", d.Condition,
			"specs", len(d.Specs), "kept", len(d.Photos), "has_description", d.Description != "",
			"spoken_price", answer.SpokenPrice, "price", d.Price, "range", rng,
			"ms", time.Since(started).Milliseconds())
		if errors.Is(cleanErr, domain.ErrNoItem) {
			return filled{Failure: domain.FailNoItem}, nil
		}
		if cleanErr != nil {
			return filled{}, fmt.Errorf("clean the answer: %w", cleanErr)
		}
		return filled{Draft: d, Range: rng}, nil
	}, restate.WithMaxRetryDuration(deadline.Sub(at)), restate.WithMaxRetryAttempts(modelAttempts))
	if err != nil {
		return fail(at, domain.FailUnavailable)
	}
	if fill.Failure != "" {
		return fail(at, fill.Failure)
	}
	if err = r.SetDraft(fill.Draft); err != nil {
		return fmt.Errorf("set draft: %w", err)
	}
	if gone, saveErr := save(at); saveErr != nil || gone {
		return errors.Join(saveErr, consume())
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
	if err = consume(); err != nil {
		return err
	}
	_, err = save(at)
	return err
}

// Expire deletes a read a day after it started. It consumes the video
// first, for a run that never ended or a read whose row is already gone.
func (s VideoRead) Expire(ctx restate.Context, in ExpireInput) error {
	id := in.Read
	loaded, err := restate.Run(ctx, func(rc restate.RunContext) (loadedRead, error) {
		r, stepErr := s.VideoReads.Get(rc, id)
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
	// A row gone before its run (the account deleted between Start and
	// Run) still leaves its video held; Start named it in the input.
	video := in.Video
	if loaded.Found {
		video = loaded.Read.VideoID()
	}
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return s.Media.Consume(rc, video)
	}); err != nil {
		return fmt.Errorf("consume video: %w", err)
	}
	if !loaded.Found {
		return nil
	}
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.VideoReads.Delete(rc, id) }); err != nil {
		return fmt.Errorf("delete read: %w", err)
	}
	return nil
}
