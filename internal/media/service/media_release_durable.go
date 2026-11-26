package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// mediaRelease is the MediaRelease service's name without the prefix.
const mediaRelease = "MediaRelease"

// PurgeAfter is how long a released photo stays public, so pages and
// thumbnails cached before the change still load.
const PurgeAfter = 24 * time.Hour

// ReleaseInput names the uploads a holder no longer shows.
type ReleaseInput struct {
	Holder uuid.UUID   `json:"holder"`
	IDs    []uuid.UUID `json:"ids"`
}

// MediaRelease deletes the objects of uploads their holder gave up: the
// original at once, since it keeps EXIF and GPS, and the public photo and
// thumbnail PurgeAfter later.
type MediaRelease struct {
	Prefix  string
	Uploads repository.Uploads
	Store   *objectstore.Store
	Now     func() time.Time
}

func (s MediaRelease) ServiceName() string { return s.Prefix + mediaRelease }

// Release sends MediaRelease one way from another module's Restate flow:
// holder, such as a listing, no longer shows ids. prefix is the calling
// flow's service prefix.
func Release(ctx restate.Context, prefix string, holder uuid.UUID, ids []uuid.UUID) {
	if len(ids) == 0 {
		return
	}
	restate.ServiceSend(ctx, prefix+mediaRelease, "Run").Send(ReleaseInput{Holder: holder, IDs: ids})
}

// Run is UC-SHR-01 A6: it releases those of in.IDs that in.Holder holds. A
// save that loses to another write, such as processing that ends meanwhile,
// reads them again.
func (s MediaRelease) Run(ctx restate.Context, in ReleaseInput) error {
	for {
		at, err := restatex.Now(ctx, s.Now)
		if err != nil {
			return err
		}
		ups, err := restate.Run(ctx, func(rc restate.RunContext) ([]domain.Upload, error) {
			return s.Uploads.GetMany(rc, in.IDs)
		})
		if err != nil {
			return fmt.Errorf("load uploads: %w", err)
		}
		var released []domain.Upload
		for i := range ups {
			if ups[i].Release(in.Holder, at) {
				released = append(released, ups[i])
			}
		}
		if len(released) == 0 {
			return nil
		}
		w := restatex.NewID(ctx, at)
		lost, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
			ptrs := make([]*domain.Upload, len(released)) // SaveAll sets the write IDs on copies
			for i := range released {
				u := released[i]
				ptrs[i] = &u
			}
			return restatex.Outcome(s.Uploads.SaveAll(rc, ptrs, w), repository.ErrConflict)
		})
		if err != nil {
			return fmt.Errorf("save uploads: %w", err)
		}
		if lost {
			continue
		}
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
			errs := make([]error, 0, len(released))
			for i := range released {
				errs = append(errs, s.Store.Delete(rc, released[i].Key()))
			}
			return errors.Join(errs...)
		}); err != nil {
			return fmt.Errorf("delete the originals: %w", err)
		}
		for i := range released {
			restate.ServiceSend(ctx, s.ServiceName(), "Purge").Send(released[i].ID(), restate.WithDelay(PurgeAfter))
		}
		return nil
	}
}

// Purge deletes a released upload's public photo and thumbnail, which
// processing that ran on while it was released may have written too.
func (s MediaRelease) Purge(ctx restate.Context, id uuid.UUID) error {
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
		return fmt.Errorf("load upload: %w", err)
	}
	if !l.Found || l.Upload.Status() != domain.StatusReleased {
		return nil
	}
	photo, thumb := domain.PublicKeys(id)
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return errors.Join(s.Store.Delete(rc, photo), s.Store.Delete(rc, thumb))
	}); err != nil {
		return fmt.Errorf("delete the public copies: %w", err)
	}
	return nil
}
