package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	aiembed "github.com/shopnexus/server/internal/ai/embed"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/provider/embed"
	"github.com/shopnexus/server/internal/seedpack"
)

func embedCmd() *cobra.Command {
	var (
		pack                  string
		text, photos, jsonOut bool
		batch                 int
	)
	cmd := &cobra.Command{
		Use:   "embed [pack]",
		Short: "Fill a pack's vectors with the stack's models",
		Long: `embed writes vectors/text.npy through the stack's EMBED_TEXT_* endpoint
(LiteLLM's embed-text, BGE-M3) and vectors/photo.npy through module ai's
SigLIP 2 from AI_EMBED_MODELS, the vectors the server would make. It sets
each listing's text_sha256 and the manifest's models. A stopped run resumes
from the last row it wrote.`,
		Example: `  seedpack embed --pack tmp/packs/vn-catalog-10k --text
  seedpack embed --pack tmp/packs/vn-catalog-10k --photos`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			u := newUI(jsonOut, true)
			ref, err := packArg(pack, args)
			if err != nil {
				return err
			}
			if !text && !photos {
				return errors.New("pass --text, --photos or both")
			}
			s, err := openStack(ctx)
			if err != nil {
				return err
			}
			defer s.close()
			dir, err := seedpack.Resolve(ctx, ref, u)
			if err != nil {
				return err
			}
			p, err := seedpack.Open(dir)
			if err != nil {
				return err
			}
			out := map[string]int{}
			if text {
				if out["text"], err = embedText(ctx, s, p, max(batch, 1), u); err != nil {
					return err
				}
			}
			if photos {
				if out["photo"], err = embedPhotos(ctx, s, p, u); err != nil {
					return err
				}
			}
			return u.result(out, func() {
				u.summary("Embedded", [][2]string{{"Text", count(out["text"])}, {"Photo", count(out["photo"])}})
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&pack, "pack", "", "the pack directory")
	f.BoolVar(&text, "text", false, "embed the listings' texts")
	f.BoolVar(&photos, "photos", false, "embed the photos")
	f.IntVar(&batch, "batch", 16, "texts per call")
	f.BoolVar(&jsonOut, "json", false, "print the counts as JSON, without bars")
	return cmd
}

// progressFile records how far an embedding run got, beside its matrix.
type progressFile struct {
	Model string `json:"model"`
	Rows  int    `json:"rows"`
	Done  int    `json:"done"`
}

// resume opens the matrix of a run of model over rows that stopped, or
// starts one.
func resume(p *seedpack.Pack, rel, model string, rows, cols int) (*seedpack.Matrix, *progressFile, error) {
	state := p.Path(rel + ".progress.json")
	var pf progressFile
	if raw, err := os.ReadFile(state); err == nil && json.Unmarshal(raw, &pf) == nil && pf.Model == model && pf.Rows == rows {
		m, err := seedpack.OpenMatrix(p.Path(rel), true)
		return m, &pf, err
	}
	m, err := seedpack.CreateMatrix(p.Path(rel), rows, cols)
	return m, &progressFile{Model: model, Rows: rows}, err
}

func saveProgress(p *seedpack.Pack, rel string, pf *progressFile) error {
	raw, _ := json.Marshal(pf)
	if err := os.WriteFile(p.Path(rel+".progress.json"), raw, 0o600); err != nil {
		return fmt.Errorf("write the progress: %w", err)
	}
	return nil
}

// embedText embeds each listing's EmbeddingText, as ListingEmbed does,
// and records its hash on the line.
func embedText(ctx context.Context, s *stack, p *seedpack.Pack, batch int, u *ui) (int, error) {
	cfg := s.cfg.Embed
	if !cfg.TextOn() {
		return 0, errors.New("text vectors are off in the stack: EMBED_TEXT_MODEL is empty")
	}
	model := embed.OpenAI{
		BaseURL: cfg.TextBaseURL, APIKey: cfg.IndexAPIKey, Model: cfg.TextModel, Dims: catalogdomain.TextDims,
		HTTP: &http.Client{Timeout: cfg.Timeout},
	}
	tree, err := catalogrepo.NewCategories(s.pool).Tree(ctx)
	if err != nil {
		return 0, fmt.Errorf("read the categories: %w", err)
	}
	names := map[string]string{}
	for _, c := range tree {
		names[c.Code] = c.DisplayName
	}
	listings, err := p.Listings()
	if err != nil {
		return 0, err
	}
	sales, err := p.History()
	if err != nil {
		return 0, err
	}
	texts := make([]string, 0, len(listings)+len(sales))
	for i := range listings {
		texts = append(texts, listings[i].Input().EmbeddingText(names[listings[i].Category]))
		listings[i].TextSHA256 = seedpack.TextSHA256(listings[i], names[listings[i].Category])
	}
	for i := range sales {
		l := &sales[i].Listing
		texts = append(texts, l.Input().EmbeddingText(names[l.Category]))
		l.TextSHA256 = seedpack.TextSHA256(*l, names[l.Category])
	}
	m, pf, err := resume(p, seedpack.TextVectors, cfg.TextVersion, len(texts), seedpack.TextDims)
	if err != nil {
		return 0, err
	}
	defer func() { _ = m.Close() }()
	bar := u.Stage("text", int64(len(texts)))
	bar.Add(int64(pf.Done))
	for pf.Done < len(texts) {
		end := min(pf.Done+batch, len(texts))
		vs, embedErr := model.Embed(ctx, texts[pf.Done:end])
		if errors.Is(embedErr, embed.ErrRefused) {
			// One at a time; a text the model refuses keeps a row of zeros.
			vs = make([][]float32, end-pf.Done)
			for i := range vs {
				one, oneErr := model.Embed(ctx, texts[pf.Done+i:pf.Done+i+1])
				if errors.Is(oneErr, embed.ErrRefused) {
					u.Warnf("row %d: the model refused the text", pf.Done+i)
					continue
				}
				if oneErr != nil {
					return pf.Done, fmt.Errorf("embed row %d: %w", pf.Done+i, oneErr)
				}
				vs[i] = one[0]
			}
		} else if embedErr != nil {
			return pf.Done, fmt.Errorf("embed rows %d to %d: %w", pf.Done, end, embedErr)
		}
		for i, v := range vs {
			if v != nil {
				if err = m.SetRow(pf.Done+i, v); err != nil {
					return pf.Done, err
				}
			}
		}
		bar.Add(int64(end - pf.Done))
		pf.Done = end
		if err = saveProgress(p, seedpack.TextVectors, pf); err != nil {
			return pf.Done, err
		}
	}
	bar.Done()
	if err = rewrite(p, seedpack.ListingsFile, listings); err != nil {
		return pf.Done, err
	}
	if sales != nil {
		if err = rewrite(p, seedpack.HistoryFile, sales); err != nil {
			return pf.Done, err
		}
	}
	p.Manifest.Vectors.Text = &seedpack.VectorSet{Model: cfg.TextVersion}
	if err = seedpack.WriteManifest(p.Dir, p.Manifest); err != nil {
		return pf.Done, err
	}
	_ = os.Remove(p.Path(seedpack.TextVectors + ".progress.json"))
	return len(texts), nil
}

// embedPhotos embeds each photo once with module ai's SigLIP 2, in the
// order listings and sales first use them.
func embedPhotos(ctx context.Context, s *stack, p *seedpack.Pack, u *ui) (int, error) {
	ai := s.cfg.AI
	if !ai.EmbedOn() {
		return 0, errors.New("photo vectors are off in the stack: AI_EMBED_MODELS is empty")
	}
	listings, err := p.Listings()
	if err != nil {
		return 0, err
	}
	sales, err := p.History()
	if err != nil {
		return 0, err
	}
	var keys []seedpack.PhotoKey
	seen := map[string]bool{}
	add := func(l seedpack.Listing) {
		for _, sha := range l.Photos {
			if !seen[sha] {
				seen[sha] = true
				keys = append(keys, seedpack.PhotoKey{Listing: l.Key, Photo: sha})
			}
		}
	}
	for i := range listings {
		l := &listings[i]
		add(*l)
	}
	for i := range sales {
		sl := &sales[i]
		add(sl.Listing)
	}
	em, err := aiembed.New(aiembed.Config{
		Models: map[string]string{ai.EmbedModel: ai.EmbedDir}, NumThreads: ai.NumThreads, MaxConcurrent: ai.EmbedMaxConcurrent,
	})
	if err != nil {
		return 0, fmt.Errorf("load %s: %w", ai.EmbedModel, err)
	}
	defer em.Close()
	m, pf, err := resume(p, seedpack.PhotoVectors, s.cfg.Embed.ImageVersion, len(keys), seedpack.PhotoDims)
	if err != nil {
		return 0, err
	}
	defer func() { _ = m.Close() }()
	if err = rewrite(p, seedpack.PhotoKeysFile, keys); err != nil {
		return 0, err
	}
	bar := u.Stage("photo", int64(len(keys)))
	bar.Add(int64(pf.Done))
	for ; pf.Done < len(keys); pf.Done++ {
		photo, readErr := p.ReadFile(seedpack.PhotoPath(keys[pf.Done].Photo))
		if readErr != nil {
			return pf.Done, readErr
		}
		res, embedErr := em.Embed(ctx, ai.EmbedModel, []aiembed.Input{{Image: photo}})
		if errors.Is(embedErr, aiembed.ErrBadImage) {
			u.Warnf("photo %s: %v", keys[pf.Done].Photo, embedErr)
		} else if embedErr != nil {
			return pf.Done, fmt.Errorf("embed photo %s: %w", keys[pf.Done].Photo, embedErr)
		} else if err = m.SetRow(pf.Done, res.Vectors[0]); err != nil {
			return pf.Done, err
		}
		bar.Add(1)
		if pf.Done%100 == 0 {
			if err = saveProgress(p, seedpack.PhotoVectors, pf); err != nil {
				return pf.Done, err
			}
		}
	}
	bar.Done()
	p.Manifest.Vectors.Photo = &seedpack.VectorSet{Model: s.cfg.Embed.ImageVersion}
	if err = seedpack.WriteManifest(p.Dir, p.Manifest); err != nil {
		return pf.Done, err
	}
	_ = os.Remove(p.Path(seedpack.PhotoVectors + ".progress.json"))
	return len(keys), nil
}

// rewrite replaces a JSONL file of the pack with lines.
func rewrite[T any](p *seedpack.Pack, rel string, lines []T) error {
	path := p.Path(rel)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", rel, err)
	}
	e := json.NewEncoder(f)
	for _, l := range lines {
		if err = e.Encode(l); err != nil {
			_ = f.Close()
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", rel, err)
	}
	return nil
}
