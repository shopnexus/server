package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	aidomain "github.com/shopnexus/server/internal/ai/domain"
	"github.com/shopnexus/server/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/llm"
)

// videoRead is the VideoRead service's name without the prefix.
const videoRead = "VideoRead"

// maxFrames is the most frames ai's Frames answers; the IDs of that many
// uploads are minted before the frames step.
const maxFrames = aidomain.MaxFrames

type StartInput struct {
	Account uuid.UUID `json:"account"`
	Video   uuid.UUID `json:"video"`
	// Key is the account's request key, which the read claims and the
	// phone's transcript waits under.
	Key uuid.UUID `json:"request_key"`
}

// Where the fill's transcript came from, for the prompt and the log line.
const (
	fromPhone  = "phone"
	fromServer = "server"
	fromNone   = "none"
)

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
	Read  domain.ListingRead `json:"read"`
	Found bool               `json:"found"`
}

// savedRead is the read a save step wrote, with its new write ID, or Gone
// when the row went meanwhile: Expire or the account's deletion took it.
type savedRead struct {
	Read domain.ListingRead `json:"read"`
	Gone bool               `json:"gone"`
}

// cutFrames is what the frames step kept: the frames saved as uploads and
// the sharpest one, or the failure ai's answer means.
type cutFrames struct {
	Frames   []domain.Photo `json:"frames,omitempty"`
	Sharpest int            `json:"sharpest"`
	Failure  domain.Failure `json:"failure,omitempty"`
}

// filled is what the fill step made of the frames and the speech. The
// transcript is not in it: it stays inside the step.
type filled struct {
	Draft   domain.Draft   `json:"draft"`
	Failure domain.Failure `json:"failure,omitempty"`
	Reason  string         `json:"reason,omitempty"`
}

// Start is UC-LST-01, step 2: it checks the account, inserts the read under
// the daily cap and holds the video, each a step, then sends Run and the
// delayed Expire. A video the hold refuses deletes the read again.
func (s VideoRead) Start(ctx restate.Context, in StartInput) (StartResult, error) {
	o, err := s.openRead(ctx, in.Account, in.Key, func(id uuid.UUID, at time.Time) domain.ListingRead {
		return domain.NewVideoRead(id, in.Account, in.Video, at)
	})
	if err != nil {
		return StartResult{}, err
	}
	if o.Answer != nil {
		return *o.Answer, nil
	}
	id, at := o.ID, o.At
	// E8: a video that is not the seller's, or that another read holds.
	holdWrite := restatex.NewID(ctx, at)
	held, err := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
		holdErr := s.Media.HoldVideo(rc, in.Account, id, in.Video, holdWrite)
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
		if err = s.deleteRead(ctx, id); err != nil {
			return StartResult{}, fmt.Errorf("undo read: %w", err)
		}
		return StartResult{Refused: held.Refusal}, nil
	}
	sendRun(ctx, s.Prefix+videoRead, id, ExpireInput{Read: id, Video: in.Video})
	return StartResult{ReadID: id}, nil
}

// Run is UC-LST-01, step 2: frames, then a fill step that streams the
// model's text to the relay, saving after each. Every end consumes the
// video; past the deadline the read fails with unavailable.
func (s VideoRead) Run(ctx restate.Context, id uuid.UUID) error {
	run, ok, err := s.loadRun(ctx, id)
	if err != nil || !ok {
		return err
	}
	r, deadline := &run.r, run.deadline
	consume := func(at time.Time) error {
		w := restatex.NewID(ctx, at)
		if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Media.Consume(rc, r.Media(), w) }); err != nil {
			return fmt.Errorf("consume video: %w", err)
		}
		return nil
	}
	// Every end consumes the video before it saves, so a read that shows
	// it ended has no video left.
	run.release = consume
	at, done, err := run.tick()
	if done {
		return err
	}
	ids := make([]uuid.UUID, maxFrames)
	for i := range ids {
		ids[i] = restatex.NewID(ctx, at)
	}
	cut, err := restate.Run(ctx, func(rc restate.RunContext) (cutFrames, error) {
		key, keyErr := s.Media.VideoKey(rc, r.Media())
		if errors.Is(keyErr, objectstore.ErrNotFound) {
			return cutFrames{Failure: domain.FailUnreadable}, nil
		}
		if keyErr != nil {
			return cutFrames{}, fmt.Errorf("find video: %w", keyErr)
		}
		callCtx, cancel := context.WithTimeout(rc, s.CallTimeout)
		defer cancel()
		res, cutErr := s.AI.Frames(callCtx, key)
		switch {
		case errors.Is(cutErr, aidomain.ErrUnreadable), errors.Is(cutErr, aidomain.ErrNotFound):
			return cutFrames{Failure: domain.FailUnreadable}, nil
		case errors.Is(cutErr, aidomain.ErrRefused):
			// ai refused the request itself, not the video.
			s.Log.WarnContext(rc, "ai refused the frames request", "read", id, "err", cutErr)
			return cutFrames{Failure: domain.FailUnavailable}, nil
		case errors.Is(cutErr, aidomain.ErrTooShort):
			return cutFrames{Failure: domain.FailTooShort}, nil
		case errors.Is(cutErr, aidomain.ErrTooLong):
			return cutFrames{Failure: domain.FailTooLong}, nil
		case cutErr != nil:
			return cutFrames{}, fmt.Errorf("cut frames: %w", cutErr)
		}
		if len(res.Frames) == 0 {
			return cutFrames{Failure: domain.FailUnreadable}, nil
		}
		res.Frames = res.Frames[:min(len(res.Frames), maxFrames)]
		out := cutFrames{Frames: make([]domain.Photo, len(res.Frames))}
		for i, f := range res.Frames {
			// The ID was minted before the step, so a retry overwrites the
			// same object and inserts no second upload.
			if saveErr := s.Media.SavePhoto(rc, r.AccountID(), ids[i], f.JPEG, "image/jpeg", mediadomain.OriginFrame); saveErr != nil {
				return cutFrames{}, fmt.Errorf("save frame %d: %w", i, saveErr)
			}
			out.Frames[i] = domain.Photo{UploadID: ids[i], AtSeconds: new(f.AtSeconds)}
			if f.Sharpness > res.Frames[out.Sharpest].Sharpness {
				out.Sharpest = i
			}
		}
		return out, nil
	}, restate.WithMaxRetryDuration(deadline.Sub(at)), restate.WithMaxRetryAttempts(modelAttempts))
	if err != nil {
		return run.fail(at, domain.FailUnavailable, "")
	}
	if cut.Failure != "" {
		return run.fail(at, cut.Failure, "")
	}
	if err = r.SetPhotos(cut.Frames); err != nil {
		return fmt.Errorf("set frames: %w", err)
	}
	if gone, saveErr := run.save(at); saveErr != nil || gone {
		return errors.Join(saveErr, consume(at))
	}

	if at, done, err = run.tick(); done {
		return err
	}
	fill, err := restate.Run(ctx, func(rc restate.RunContext) (filled, error) {
		started := time.Now()
		transcript, trErr := s.Reads.Transcript(rc, id)
		if trErr != nil {
			return filled{}, fmt.Errorf("read the transcript: %w", trErr)
		}
		from := fromPhone
		if transcript == "" {
			from = fromServer
			key, keyErr := s.Media.VideoKey(rc, r.Media())
			if errors.Is(keyErr, objectstore.ErrNotFound) {
				return filled{Failure: domain.FailUnreadable}, nil
			}
			if keyErr != nil {
				return filled{}, fmt.Errorf("find video: %w", keyErr)
			}
			sttCtx, cancelSTT := context.WithTimeout(rc, s.CallTimeout)
			heard, sttErr := s.AI.Transcribe(sttCtx, key)
			cancelSTT()
			switch {
			case errors.Is(sttErr, aidomain.ErrUnreadable):
				// No audio track: the read goes on without speech.
			case errors.Is(sttErr, aidomain.ErrNotFound):
				return filled{Failure: domain.FailUnreadable}, nil
			case errors.Is(sttErr, aidomain.ErrTooLong), errors.Is(sttErr, aidomain.ErrRefused):
				// The frames step read the video, so a refusal is ai's limit
				// or a bad request: read without speech.
				s.Log.WarnContext(rc, "speech to text refused; the read goes on without speech", "read", id, "err", sttErr)
			case sttErr != nil:
				return filled{}, fmt.Errorf("transcribe: %w", sttErr)
			default:
				transcript = heard
			}
			if strings.TrimSpace(transcript) == "" {
				from = fromNone
			}
		}
		images := make([][]byte, len(r.Photos()))
		for i, f := range r.Photos() {
			img, imgErr := s.Media.QueryImage(rc, r.AccountID(), f.UploadID)
			if imgErr != nil {
				return filled{}, fmt.Errorf("read frame %d: %w", i, imgErr)
			}
			images[i] = img
		}
		rep, modelErr := s.writeReply(rc, id, llm.Request{
			System: systemPrompt, User: userPrompt(transcript, len(images), from), Images: images,
		}, mediaTries, func(so string) domain.Text { return domain.PartialText(so, catalogdomain.MaxDescription) })
		if refusedModel(modelErr) {
			s.Log.WarnContext(rc, "the model refused the video read", "read", id, "err", modelErr)
			return filled{Failure: domain.FailUnavailable, Reason: modelReason(modelErr)}, nil
		}
		if modelErr != nil {
			return filled{}, modelErr
		}
		d, cleanErr := domain.VideoDraft(rep, r.Photos(), cut.Sharpest)
		if cleanErr == nil {
			d.CategoryCode = s.guessCategory(rc, id, d)
		}
		// One line per answer, with no word of the seller's: no transcript,
		// title or description.
		s.Log.InfoContext(rc, "video read filled", "read", id, "prompt_version", PromptVersion,
			"frames", len(images), "transcript_chars", utf8.RuneCountInString(transcript), "transcript_from", from,
			"is_item", cleanErr == nil, "category", d.CategoryCode, "kept", len(d.Photos),
			"description_chars", utf8.RuneCountInString(d.Description), "ms", time.Since(started).Milliseconds())
		if noItem, ok := errors.AsType[domain.NoItemError](cleanErr); ok {
			return filled{Failure: domain.FailNoItem, Reason: noItem.Reason}, nil
		}
		if cleanErr != nil {
			return filled{}, fmt.Errorf("clean the answer: %w", cleanErr)
		}
		return filled{Draft: d}, nil
	}, restate.WithMaxRetryDuration(deadline.Sub(at)), restate.WithMaxRetryAttempts(modelAttempts))
	if err != nil {
		return run.fail(at, domain.FailUnavailable, modelReason(err))
	}
	if fill.Failure != "" {
		return run.fail(at, fill.Failure, fill.Reason)
	}
	if err = r.SetDraft(fill.Draft, false); err != nil {
		return fmt.Errorf("set draft: %w", err)
	}
	return run.finish(at)
}

// Expire deletes a read a day after it started. It consumes the video
// first, for a run that never ended or a read whose row is already gone.
func (s VideoRead) Expire(ctx restate.Context, in ExpireInput) error {
	id := in.Read
	loaded, err := s.loadStep(ctx, id)
	if err != nil {
		return err
	}
	// A row gone before its run (the account deleted between Start and
	// Run) still leaves its video held; Start named it in the input.
	video := in.Video
	if loaded.Found {
		video = loaded.Read.Media()
	}
	at, nowErr := restatex.Now(ctx, s.Now)
	if nowErr != nil {
		return nowErr
	}
	w := restatex.NewID(ctx, at)
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return s.Media.Consume(rc, video, w)
	}); err != nil {
		return fmt.Errorf("consume video: %w", err)
	}
	if !loaded.Found {
		return nil
	}
	return s.expireRead(ctx, id)
}
