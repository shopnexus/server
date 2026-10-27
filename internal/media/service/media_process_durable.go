package service

import (
	"bytes"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/objectstore"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// mediaProcess is the MediaProcess service's name without the prefix.
const mediaProcess = "MediaProcess"

// ProcessResult is how processing an upload ended. A refused photo is a
// failed result, not a refusal: the listing that holds it shows the reason.
type ProcessResult struct {
	Status   domain.Status `json:"status"`
	PhotoKey string        `json:"photo_key,omitempty"`
	ThumbKey string        `json:"thumb_key,omitempty"`
	Failure  string        `json:"failure,omitempty"`
}

func (ProcessResult) Refusal() *apperr.Error { return nil }

// MediaProcess turns an attached upload into a public photo and thumbnail,
// or a square.
// The photo's bytes never enter the journal: the processing step writes
// both objects itself and returns their keys.
type MediaProcess struct {
	Prefix  string
	Uploads repository.Uploads
	Store   *objectstore.Store
	Now     func() time.Time
	// Slots bounds how many photos the process decodes at once; a decoded
	// photo takes up to 160 MB.
	Slots chan struct{}
}

func (s MediaProcess) ServiceName() string { return s.Prefix + mediaProcess }

// processed is what the processing step decided.
type processed struct {
	PhotoKey string `json:"photo_key"`
	ThumbKey string `json:"thumb_key"`
	Failure  string `json:"failure"`
}

// Run processes the upload once; run again, it returns what the first run
// decided.
func (s MediaProcess) Run(ctx restate.Context, id uuid.UUID) (ProcessResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return ProcessResult{}, err
	}
	l, err := restate.Run(ctx, func(rc restate.RunContext) (loadedUpload, error) {
		u, stepErr := s.Uploads.Get(rc, id)
		if errors.Is(stepErr, repository.ErrNotFound) {
			return loadedUpload{}, nil
		}
		if stepErr != nil {
			return loadedUpload{}, fmt.Errorf("get upload: %w", stepErr)
		}
		return loadedUpload{Upload: u, Found: true}, nil
	})
	if err != nil {
		return ProcessResult{}, fmt.Errorf("load upload: %w", err)
	}
	u := l.Upload
	switch {
	case !l.Found:
		return ProcessResult{Status: domain.StatusFailed, Failure: "unknown upload"}, nil
	case u.Status() == domain.StatusReady || u.Status() == domain.StatusFailed:
		return ProcessResult{Status: u.Status(), PhotoKey: u.PhotoKey(), ThumbKey: u.ThumbKey(), Failure: u.Failure()}, nil
	case u.Status() != domain.StatusAttached:
		return ProcessResult{Status: domain.StatusFailed, Failure: "the upload is not attached"}, nil
	}

	out, err := restate.Run(ctx, func(rc restate.RunContext) (processed, error) {
		select {
		case s.Slots <- struct{}{}:
			defer func() { <-s.Slots }()
		case <-rc.Done():
			return processed{}, fmt.Errorf("wait for a processing slot: %w", rc.Err())
		}
		r, stepErr := s.Store.Get(rc, u.Key())
		if stepErr != nil {
			return processed{}, fmt.Errorf("read the upload: %w", stepErr)
		}
		defer func() { _ = r.Close() }() // a read-only object; nothing to flush
		photo, thumb, stepErr := process(r, u.ContentType(), u.Shape())
		if reason := refusal(stepErr); reason != "" {
			return processed{Failure: reason}, nil
		}
		if stepErr != nil {
			return processed{}, stepErr
		}
		photoKey := "media/" + id.String() + ".jpg"
		if stepErr = s.Store.Put(rc, photoKey, "image/jpeg", bytes.NewReader(photo), int64(len(photo))); stepErr != nil {
			return processed{}, fmt.Errorf("write the photo: %w", stepErr)
		}
		if thumb == nil { // a square has no thumbnail
			return processed{PhotoKey: photoKey}, nil
		}
		thumbKey := "media/" + id.String() + "_thumb.jpg"
		if stepErr = s.Store.Put(rc, thumbKey, "image/jpeg", bytes.NewReader(thumb), int64(len(thumb))); stepErr != nil {
			return processed{}, fmt.Errorf("write the thumbnail: %w", stepErr)
		}
		return processed{PhotoKey: photoKey, ThumbKey: thumbKey}, nil
	})
	if err != nil {
		return ProcessResult{}, fmt.Errorf("process the photo: %w", err)
	}
	if out.Failure != "" {
		u.Failed(out.Failure, at)
	} else {
		u.Processed(out.PhotoKey, out.ThumbKey, at)
	}
	w := restatex.NewID(ctx, at)
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		u := u // Save sets the write ID on its copy; u outside stays as journaled
		return restatex.Ignore(s.Uploads.Save(rc, &u, w), repository.ErrConflict)
	}); err != nil {
		return ProcessResult{}, fmt.Errorf("save upload: %w", err)
	}
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Store.Delete(rc, u.Key()) }); err != nil {
		return ProcessResult{}, fmt.Errorf("delete the original: %w", err)
	}
	return ProcessResult{Status: u.Status(), PhotoKey: u.PhotoKey(), ThumbKey: u.ThumbKey(), Failure: u.Failure()}, nil
}
