//go:build eval

package eval_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"text/tabwriter"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/ai"
	aisvc "github.com/shopnexus/server2/internal/ai/service"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server2/internal/catalog/repository"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	discoverydomain "github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/eval"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/media/mediatest"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/platform/testenv"
	"github.com/shopnexus/server2/internal/provider/embed"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// k is the depth the set is scored at.
const k = 10

type result struct {
	q            eval.Query
	got          []string
	recall, ndcg float64
}

// TestSearchEval posts the sample listings through catalog with the real
// models, waits until ListingEmbed has given each its vectors, runs every
// query through discovery and prints recall@10 and nDCG@10. It fails on
// errors, never on scores: the scores are for tuning the RRF weights and the
// floors.
func TestSearchEval(t *testing.T) { //nolint:paralleltest // one run, against shared real models
	cfg, err := config.FromDotEnv("../../../.env")
	if err != nil {
		t.Fatalf("the developer's .env, with LLM_* and EMBED_* for real models: %v", err)
	}
	listings, queries, err := eval.Load("testdata")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	store := objectstoretest.New(t)
	m := mediatest.Service(db, store, in, p)
	text := func(key string) embed.OpenAI {
		return embed.OpenAI{
			BaseURL: cfg.Embed.TextBaseURL, APIKey: key, Model: cfg.Embed.TextModel, Dims: catalogdomain.TextDims,
			HTTP: &http.Client{Timeout: cfg.Embed.Timeout},
		}
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	// SigLIP 2 runs here, in module ai, from the models AI_* names.
	engines, err := ai.Load(cfg.AI)
	if err != nil {
		t.Fatalf("module ai's models (scripts/fetch-models.sh): %v", err)
	}
	t.Cleanup(engines.Close)
	aim := ai.Mount(module.Deps{Ingress: in, ServicePrefix: p, Log: log}, ai.Settings{Engines: engines, Objects: store, Config: cfg.AI})
	photos := aisvc.Photos{AI: aim.Service, Dims: catalogdomain.PhotoDims, Timeout: cfg.Embed.Timeout}
	c := catalogtest.Service(db, accounttest.Service(db), m, in, p, func(d *catalogsvc.Deps) {
		d.Text, d.Image = text(cfg.Embed.IndexAPIKey), photos
		d.TextVersion, d.ImageVersion, d.EmbedSlots, d.Log = cfg.Embed.TextVersion, cfg.Embed.ImageVersion, cfg.Embed.Slots, log
	})
	restatetest.Start(t, slices.Concat(m.Durable(), c.Durable(), aim.Services)...)
	d := service.New(service.Deps{
		Saves: repository.NewSaves(db), Understandings: repository.NewUnderstandings(db),
		History: repository.NewHistory(db), Hidden: repository.NewHidden(db), Watches: repository.NewWatches(db), Accounts: accounttest.Service(db),
		QueryVectors: repository.NewQueryVectors(db), PhotoVectors: repository.NewPhotoVectors(db),
		Catalog: c, Media: m,
		LLM:         llm.OpenAI{BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey, Model: cfg.LLM.Model, HTTP: &http.Client{}},
		Text:        text(cfg.Embed.SearchAPIKey),
		Image:       photos,
		TextVersion: cfg.Embed.TextVersion, ImageVersion: cfg.Embed.ImageVersion,
		TextMin: cfg.Embed.TextMinSimilarity, PhotoMin: cfg.Embed.PhotoMinSimilarity,
		ModelTimeout: discoverydomain.ModelTimeout, Log: log, Now: time.Now,
	})

	ids := map[string]uuid.UUID{}
	keys := map[uuid.UUID]string{}
	for _, l := range listings {
		seller := accounttest.WithPhone(t, db)
		photo := mediatest.Upload(t, m, seller, "image/jpeg", read(t, l.Key+".jpg"))
		v, postErr := c.Post(ctx, seller, uuid.NewV7().String(), catalogdomain.ListingInput{
			Title: l.Title, Price: l.Price, Description: l.Description, CategoryCode: l.Category,
			Condition: catalogdomain.Condition(l.Condition), AllowOffers: true, CommuneCode: "00004",
			Photos: []uuid.UUID{photo}, Specs: l.Specs,
		})
		if postErr != nil {
			t.Fatalf("post %s: %v", l.Key, postErr)
		}
		ids[l.Key], keys[v.ID] = v.ID, l.Key
	}
	vectors := catalogrepo.NewVectors(db)
	started := time.Now()
	testenv.Eventually(t, 20*time.Minute, func() error {
		for key, id := range ids {
			st, stateErr := vectors.State(ctx, id, cfg.Embed.TextVersion, cfg.Embed.ImageVersion)
			if stateErr != nil {
				return stateErr
			}
			if st.TextHash == nil || len(st.Photos) == 0 {
				return fmt.Errorf("%s has no vectors yet; are OpenRouter and module ai's models there?", key)
			}
		}
		return nil
	})
	t.Logf("%d listings embedded in %s", len(ids), time.Since(started).Round(time.Second))

	viewer := accounttest.WithPhone(t, db)
	results := make([]result, 0, len(queries))
	for _, q := range queries {
		input := service.SearchInput{Limit: k}
		var who *uuid.UUID
		switch q.Kind {
		case "q":
			input.Query = q.Q
		case "photo":
			input.Photo, who = mediatest.Upload(t, m, viewer, "image/jpeg", read(t, q.Photo)), &viewer
		case "like":
			input.Like = ids[q.Like]
		default:
			t.Fatalf("query %s: kind %q", q.Name, q.Kind)
		}
		v, searchErr := d.Search(ctx, who, input)
		if searchErr != nil {
			t.Fatalf("%s: %v", q.Name, searchErr)
		}
		got := make([]string, len(v.Items))
		for i, it := range v.Items {
			got[i] = keys[it.ID]
		}
		results = append(results, result{q: q, got: got, recall: eval.RecallAt(got, q.Expect, k), ndcg: eval.NDCGAt(got, q.Expect, k)})
	}

	report := table(results)
	t.Log("\n" + report)
	out := filepath.Join("..", "..", "..", "tmp", "search-eval-"+time.Now().UTC().Format("20060102T150405Z")+".txt")
	if err = os.MkdirAll(filepath.Dir(out), 0o755); err == nil {
		err = os.WriteFile(out, []byte(report), 0o644)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("written to %s", out)
}

// table is one line per query, then the mean recall and nDCG of each kind
// and of all queries.
func table(rs []result) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "kind\tquery\trecall@10\tnDCG@10\texpected\tfound")
	type sum struct {
		n            int
		recall, ndcg float64
	}
	sums := map[string]*sum{"all": {}}
	for i := range rs {
		r := &rs[i]
		fmt.Fprintf(w, "%s\t%s\t%.2f\t%.2f\t%s\t%s\n", r.q.Kind, r.q.Name, r.recall, r.ndcg,
			strings.Join(r.q.Expect, " "), strings.Join(r.got, " "))
		if sums[r.q.Kind] == nil {
			sums[r.q.Kind] = &sum{}
		}
		for _, s := range []*sum{sums[r.q.Kind], sums["all"]} {
			s.n++
			s.recall += r.recall
			s.ndcg += r.ndcg
		}
	}
	fmt.Fprintln(w, "\t\t\t\t\t")
	for _, kind := range []string{"q", "photo", "like", "all"} {
		if s := sums[kind]; s != nil && s.n > 0 {
			fmt.Fprintf(w, "%s\tmean of %d\t%.3f\t%.3f\t\t\n", kind, s.n, s.recall/float64(s.n), s.ndcg/float64(s.n))
		}
	}
	_ = w.Flush()
	return b.String()
}

func read(t *testing.T, file string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "photos", file))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
