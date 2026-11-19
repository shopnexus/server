//go:build eval

package service_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/provider/embed"
)

type guessSample struct{ Category, Title, Description string }

// guessSamples are the listings of the search sample set and the demo pack.
func guessSamples(t *testing.T) []guessSample {
	t.Helper()
	var out []guessSample
	b, err := os.ReadFile("../../discovery/eval/testdata/listings.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("../../../cmd/devseed/data/demo/listings.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var s guessSample
		if err = json.Unmarshal(sc.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// TestGuessEval guesses each sample listing's category with the real text
// model at EMBED_TEXT_BASE_URL, EMBED_TEXT_MODEL and EMBED_INDEX_API_KEY,
// and prints the share it gets right. A sample in Khác is left out: no text
// is guessed into it. It fails on errors, never on the score.
func TestGuessEval(t *testing.T) { //nolint:paralleltest // one run against the real model
	url, model := os.Getenv("EMBED_TEXT_BASE_URL"), os.Getenv("EMBED_TEXT_MODEL")
	if url == "" || model == "" {
		t.Skip("EMBED_TEXT_BASE_URL and EMBED_TEXT_MODEL are not set")
	}
	text := embed.OpenAI{
		BaseURL: url, APIKey: os.Getenv("EMBED_INDEX_API_KEY"), Model: model, Dims: domain.TextDims,
		HTTP: &http.Client{Timeout: 30 * time.Second},
	}
	e := newEnv(t, func(d *service.Deps) { d.Text = text })
	ctx := context.Background()
	var n, titleHits, hits int
	for _, s := range guessSamples(t) {
		if strings.HasSuffix(s.Category, ".other") {
			continue
		}
		n++
		byTitle, err := e.catalog.GuessCategory(ctx, s.Title, "")
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.catalog.GuessCategory(ctx, s.Title, s.Description)
		if err != nil {
			t.Fatal(err)
		}
		if byTitle == s.Category {
			titleHits++
		}
		if got == s.Category {
			hits++
		} else {
			t.Logf("%-20s guessed %-20s %s", s.Category, got, s.Title)
		}
	}
	t.Logf("%d listings: title and description %d right (%.0f%%), title alone %d (%.0f%%)",
		n, hits, 100*float64(hits)/float64(n), titleHits, 100*float64(titleHits)/float64(n))
}
