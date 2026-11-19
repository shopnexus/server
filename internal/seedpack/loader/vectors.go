package loader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server/internal/catalog/repository"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/seedpack"
)

// vectors writes the pack's usable vectors for listings that lack them and
// sends the other listings to ListingEmbed.
func (r *run) vectors(ctx context.Context) error {
	if r.pl.opts.Vectors == VectorsNone || (r.d.TextVersion == "" && r.d.ImageVersion == "") {
		return nil
	}
	rows := make([]row, 0, len(r.pl.onSale)+len(r.pl.sales))
	rows = append(rows, r.pl.onSale...)
	for i := range r.pl.sales {
		s := &r.pl.sales[i]
		rows = append(rows, s.row)
	}
	there, err := existing(ctx, r.d.Pool, "listings", idsOf(rows, func(x row) uuid.UUID { return x.id }))
	if err != nil {
		return err
	}
	rows = slices.DeleteFunc(rows, func(x row) bool { return !there[x.id] })

	var text, photo *seedpack.Matrix
	if r.pl.textOn {
		if text, err = seedpack.OpenMatrix(r.pl.src.Path(seedpack.TextVectors), false); err != nil {
			return err
		}
		defer func() { _ = text.Close() }() // read-only
	}
	photoRow := map[string]int{}
	if r.pl.photoOn {
		keys, keysErr := r.pl.src.PhotoKeys()
		if keysErr != nil {
			return keysErr
		}
		for i, k := range keys {
			photoRow[k.Photo] = i
		}
		if photo, err = seedpack.OpenMatrix(r.pl.src.Path(seedpack.PhotoVectors), false); err != nil {
			return err
		}
		defer func() { _ = photo.Close() }() // read-only
	}

	vectors := catalogrepo.NewVectors(r.d.Pool)
	var mu sync.Mutex
	var refresh []uuid.UUID
	now := r.d.Now()
	err = stage(ctx, r.prog, "vectors", max(r.pl.opts.Workers, 1), rows, func(ctx context.Context, x row) error {
		have, stateErr := vectors.State(ctx, x.id, r.d.TextVersion, r.d.ImageVersion)
		if stateErr != nil {
			return fmt.Errorf("vectors of %s: %w", x.Key, stateErr)
		}
		in := x.Input()
		textNow := in.EmbeddingText(r.pl.names[x.Category])
		hash := catalogdomain.TextHash(r.d.TextVersion, textNow)
		needs := false
		if r.d.TextVersion != "" && !bytes.Equal(have.TextHash, hash) {
			v, textErr := r.textVector(text, x, textNow)
			if textErr != nil {
				return textErr
			}
			if v == nil {
				needs = true
			} else {
				if putErr := vectors.PutText(ctx, x.id, r.d.TextVersion, hash, v, now); putErr != nil {
					return fmt.Errorf("text vector of %s: %w", x.Key, putErr)
				}
				r.counted(&r.res.Vectors.Text)
			}
		}
		for i, sha := range x.Photos {
			if r.d.ImageVersion == "" || (r.pl.opts.FirstPhotoOnly && i > 0) {
				break
			}
			key := "media/" + seedpack.ID(r.name, "upload", fmt.Sprintf("%s/%d", x.Key, i), x.CreatedAt).String() + ".jpg"
			if slices.Contains(have.Photos, key) {
				continue
			}
			at, ok := photoRow[sha]
			if !r.pl.photoOn || !ok {
				needs = true
				continue
			}
			v, rowErr := photo.Row(at)
			if rowErr != nil {
				return rowErr
			}
			if v == nil {
				needs = true
				continue
			}
			if putErr := vectors.PutPhoto(ctx, x.id, key, r.d.ImageVersion, v, now); putErr != nil {
				return fmt.Errorf("photo vector of %s: %w", x.Key, putErr)
			}
			r.counted(&r.res.Vectors.Photo)
		}
		if needs {
			mu.Lock()
			refresh = append(refresh, x.id)
			mu.Unlock()
		}
		return nil
	})
	if err != nil {
		return err
	}
	return r.refresh(ctx, refresh)
}

// textVector is the pack's text vector of x when it still fits x's text:
// any legacy vector, else one made from the same text. Nil leaves it to
// ListingEmbed.
func (r *run) textVector(m *seedpack.Matrix, x row, text string) ([]float32, error) {
	if m == nil {
		return nil, nil
	}
	if r.pl.Pack.Vectors.Text.Origin != seedpack.OriginLegacy {
		sum := sha256.Sum256([]byte(text))
		if x.TextSHA256 != hex.EncodeToString(sum[:]) {
			return nil, nil
		}
	}
	v, err := m.Row(x.line)
	if err != nil {
		return nil, fmt.Errorf("text vector of %s: %w", x.Key, err)
	}
	return v, nil
}

func (r *run) counted(n *int) {
	r.mu.Lock()
	*n++
	r.mu.Unlock()
}

// refresh sends each listing to ListingEmbed, one every EmbedEvery, as
// cmd/search-reindex does.
func (r *run) refresh(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	if r.d.Ingress == nil {
		r.warnf("%d listings need vectors and the load has no Restate ingress; run search-reindex", len(ids))
		return nil
	}
	slices.SortFunc(ids, uuid.UUID.Compare)
	bar := r.prog.Stage("embed requests", int64(len(ids)))
	defer bar.Done()
	name := r.d.Prefix + catalogsvc.ListingEmbedName
	for i, id := range ids {
		_, err := ingress.ObjectSend[restate.Void](r.d.Ingress, name, id.String(), "Refresh").
			Send(ctx, restate.Void{}, restate.WithDelay(time.Duration(i)*r.pl.opts.EmbedEvery))
		if err != nil {
			return fmt.Errorf("send %s to %s: %w", id, name, err)
		}
		bar.Add(1)
	}
	r.res.Vectors.Refreshes = len(ids)
	return nil
}
