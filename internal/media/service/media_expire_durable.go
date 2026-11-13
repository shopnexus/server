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

// expireNear is how close to its expiry a run sleeps out the rest instead of
// sending itself again.
const expireNear = time.Minute

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

// Run expires the upload when it is still waiting at its expiry and
// deletes its object. A waiting upload whose expiry a draft moved out sends
// this run again for that moment; an attached, processed or unknown upload
// is left as it is. A save that loses to another write reads the upload
// again and goes on from what it finds, so the chain never ends while the
// upload waits.
func (s MediaExpire) Run(ctx restate.Context, id uuid.UUID) error {
	for {
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
		if !l.Found {
			return nil
		}
		if left := u.ExpiresAt().Sub(at); u.Status() == domain.StatusWaiting && left > 0 {
			if left < expireNear {
				// A wake a moment early, by this clock against the sender's:
				// a send keyed for the same moment would attach to this run.
				if err = restate.Sleep(ctx, left); err != nil {
					return fmt.Errorf("sleep until the expiry: %w", err)
				}
				continue
			}
			// The upload ID keyed the first run, so the key names the moment: a
			// second wake for the same expiry sends nothing more.
			restate.ServiceSend(ctx, s.ServiceName(), "Run").Send(id, restate.WithDelay(left),
				restate.WithIdempotencyKey(fmt.Sprintf("%s/%d", id, u.ExpiresAt().Unix())))
			return nil
		}
		if !u.Expire(at) {
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
			continue // an attach ends the run at the next load; an extension sends it again
		}
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Store.Delete(rc, u.Key()) }); err != nil {
			return fmt.Errorf("delete the upload's object: %w", err)
		}
		return nil
	}
}
