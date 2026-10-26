package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/repository"
	"github.com/shopnexus/server2/internal/platform/objectstore"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// mediaExpire is the MediaExpire service's name without the prefix.
const mediaExpire = "MediaExpire"

// MediaExpire deletes an upload that nothing attached within a day.
// CreateUpload schedules it with a delay.
type MediaExpire struct {
	Prefix  string
	Uploads repository.Uploads
	Store   *objectstore.Store
	Now     func() time.Time
}

func (s MediaExpire) ServiceName() string { return s.Prefix + mediaExpire }

// loadedUpload is the upload as the load step read it.
type loadedUpload struct {
	Upload domain.Upload `json:"upload"`
	Found  bool          `json:"found"`
}

// Run expires the upload when it is still waiting and deletes its object;
// an attached, processed or unknown upload is left as it is.
func (s MediaExpire) Run(ctx restate.Context, id uuid.UUID) error {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return err
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
		return fmt.Errorf("load upload: %w", err)
	}
	u := l.Upload
	if !l.Found || !u.Expire(at) {
		return nil
	}
	w := restatex.NewID(ctx, at)
	lost, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		u := u // Save sets the write ID on its copy; u outside stays as journaled
		return restatex.Outcome(s.Uploads.Save(rc, &u, w), repository.ErrConflict)
	})
	if err != nil {
		return fmt.Errorf("save upload: %w", err)
	}
	if lost {
		return nil // attached meanwhile
	}
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Store.Delete(rc, u.Key()) }); err != nil {
		return fmt.Errorf("delete the upload's object: %w", err)
	}
	return nil
}
