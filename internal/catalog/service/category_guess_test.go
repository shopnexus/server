package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/provider/embed/embedtest"
)

func TestGuessCategory(t *testing.T) {
	t.Parallel()
	text := embedtest.New(domain.TextDims)
	e := newEnv(t, func(d *service.Deps) { d.Text = text })
	ctx := context.Background()
	bicycle := embedtest.Axis(domain.TextDims, 0)
	text.Set("Xe cộ › Xe đạp: xe đạp thể thao", bicycle)
	text.Set(domain.GuessText("Giant Escape 3", "Size M"), bicycle)

	got, err := e.catalog.GuessCategory(ctx, "Giant Escape 3", "Size M")
	if err != nil || got != "vehicles.bicycle" {
		t.Fatalf("guess = %q, %v", got, err)
	}
	// Every subcategory with keywords embeds once per process.
	calls := text.Calls()
	if _, err = e.catalog.GuessCategory(ctx, "Giant Escape 3", ""); err != nil || text.Calls() != calls+1 {
		t.Fatalf("second guess: %d calls after %d, %v", text.Calls(), calls, err)
	}
	if got, err = e.catalog.GuessCategory(ctx, " ", ""); err != nil || got != "" || text.Calls() != calls+1 {
		t.Fatalf("a blank text: %q, %v", got, err)
	}
	text.SetFail(errors.New("down"))
	if _, err = e.catalog.GuessCategory(ctx, "Giant Escape 3", ""); err == nil {
		t.Fatal("a failed model guessed")
	}
}

func TestGuessCategoryOff(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(d *service.Deps) { d.Text = nil })
	if got, err := e.catalog.GuessCategory(context.Background(), "Giant Escape 3", ""); err != nil || got != "" {
		t.Fatalf("text model off: %q, %v", got, err)
	}
}

// Every subcategory with keywords is in the category tree, so none is
// guessed into a code a listing cannot take.
func TestKeywordsNameLeavesOfTheTree(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tree, err := e.catalog.Categories(context.Background(), locale.Vietnamese)
	if err != nil {
		t.Fatal(err)
	}
	leaf := map[string]bool{}
	for _, c := range tree {
		leaf[c.Code] = c.ParentCode != ""
	}
	for code := range domain.CategoryKeywords {
		if !leaf[code] {
			t.Errorf("%s is not a subcategory", code)
		}
	}
}
