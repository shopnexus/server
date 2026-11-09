package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/objectstore"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/embed"
)

// ListingEmbedName is the ListingEmbed object's name without the prefix;
// cmd/search-reindex sends to it.
const ListingEmbedName = "ListingEmbed"

// ListingEmbed keeps one listing's search vectors in line with its content.
// It is a Virtual Object keyed by the listing ID: Restate runs one Refresh
// per listing at a time, so a burst of edits queues, the first run embeds
// the newest content and the others find nothing to do.
type ListingEmbed struct {
	Prefix                    string
	Listings                  repository.Listings
	Categories                repository.Categories
	Vectors                   repository.Vectors
	Media                     mediasvc.Service
	Text, Image               embed.Model
	TextVersion, ImageVersion string
	Slots                     chan struct{}
	Log                       *slog.Logger
	Now                       func() time.Time
}

func (s ListingEmbed) ServiceName() string { return s.Prefix + ListingEmbedName }

// embedPlan is what the load step found: the text to embed when its hash
// moved, the processed photos without a vector, and every photo key, to
// drop the vectors of photos the listing no longer has.
type embedPlan struct {
	Found    bool     `json:"found"`
	Text     string   `json:"text,omitempty"`
	TextHash []byte   `json:"text_hash,omitempty"`
	Missing  []string `json:"missing,omitempty"`
	Keys     []string `json:"keys,omitempty"`
}

// Refresh reads the listing as it is now and brings its vectors in line, a
// step for the text and one for each photo, so a retry goes on from the
// photo it reached. A listing that is gone, deleted, processing or rejected
// has nothing to embed: processing ends in ListingPublish, which sends a
// Refresh of its own, and a deleted listing keeps its row and the vectors
// it had, which search's status filter leaves out.
func (s ListingEmbed) Refresh(ctx restate.ObjectContext) error {
	id, err := uuid.Parse(restate.Key(ctx))
	if err != nil {
		//nolint:wrapcheck // Restate must receive the terminal error itself
		return restate.TerminalErrorf("listing key %q: %v", restate.Key(ctx), err)
	}
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return err
	}
	p, err := restate.Run(ctx, func(rc restate.RunContext) (embedPlan, error) {
		l, stepErr := s.Listings.Get(rc, id)
		if errors.Is(stepErr, repository.ErrNotFound) {
			return embedPlan{}, nil
		}
		if stepErr != nil {
			return embedPlan{}, fmt.Errorf("get listing: %w", stepErr)
		}
		switch l.Status() { //nolint:exhaustive // the other statuses are embedded
		case domain.StatusDeleted, domain.StatusProcessing, domain.StatusRejected:
			return embedPlan{}, nil
		}
		tree, stepErr := s.Categories.Tree(rc)
		if stepErr != nil {
			return embedPlan{}, fmt.Errorf("read categories: %w", stepErr)
		}
		name := l.CategoryCode()
		for _, c := range tree {
			if c.Code == l.CategoryCode() {
				name = c.DisplayName
			}
		}
		have, stepErr := s.Vectors.State(rc, id, s.TextVersion, s.ImageVersion)
		if stepErr != nil {
			return embedPlan{}, fmt.Errorf("read vector state: %w", stepErr)
		}
		plan := embedPlan{Found: true, Keys: l.ProcessedPhotos()}
		text := l.EmbeddingText(name)
		if hash := domain.TextHash(s.TextVersion, text); !bytes.Equal(hash, have.TextHash) {
			plan.Text, plan.TextHash = text, hash
		}
		for _, k := range plan.Keys {
			if !slices.Contains(have.Photos, k) {
				plan.Missing = append(plan.Missing, k)
			}
		}
		return plan, nil
	})
	if err != nil {
		return fmt.Errorf("plan the embedding: %w", err)
	}
	if !p.Found {
		return nil
	}
	if p.TextHash != nil {
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
			release, slotErr := s.slot(rc)
			if slotErr != nil {
				return slotErr
			}
			defer release()
			vs, embedErr := s.Text.Embed(rc, []string{p.Text})
			if errors.Is(embedErr, embed.ErrRefused) {
				s.Log.WarnContext(rc, "the text model refused a listing", "listing", id, "err", embedErr)
				return nil
			}
			if embedErr != nil {
				return fmt.Errorf("embed text: %w", embedErr)
			}
			return s.Vectors.PutText(rc, id, s.TextVersion, p.TextHash, vs[0], at)
		}); err != nil {
			return fmt.Errorf("text vector: %w", err)
		}
	}
	for _, key := range p.Missing {
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
			// The slot comes first, so a backfill holds one photo per slot.
			release, slotErr := s.slot(rc)
			if slotErr != nil {
				return slotErr
			}
			defer release()
			photo, readErr := s.Media.ReadPhoto(rc, key)
			if errors.Is(readErr, objectstore.ErrNotFound) {
				return nil // the photo went with an edit; the drop step clears it
			}
			if readErr != nil {
				return fmt.Errorf("read photo: %w", readErr)
			}
			vs, embedErr := s.Image.Embed(rc, []string{embed.ImageInput(photo)})
			if errors.Is(embedErr, embed.ErrRefused) {
				s.Log.WarnContext(rc, "the image model refused a photo", "listing", id, "photo", key, "err", embedErr)
				return nil
			}
			if embedErr != nil {
				return fmt.Errorf("embed photo: %w", embedErr)
			}
			return s.Vectors.PutPhoto(rc, id, key, s.ImageVersion, vs[0], at)
		}); err != nil {
			return fmt.Errorf("photo vector %s: %w", key, err)
		}
	}
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return s.Vectors.DropPhotosExcept(rc, id, p.Keys)
	}); err != nil {
		return fmt.Errorf("drop removed photos: %w", err)
	}
	return nil
}

// slot waits for one of the process's embedding slots; both embed steps
// call it.
func (s ListingEmbed) slot(ctx context.Context) (release func(), err error) {
	select {
	case s.Slots <- struct{}{}:
		return func() { <-s.Slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for an embedding slot: %w", ctx.Err())
	}
}
