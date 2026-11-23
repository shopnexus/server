package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/locale"
)

// guessBatch is how many category texts go in one embedding call.
const guessBatch = 64

// GuessCategory embeds the listing's text with the text model and takes
// the nearest category text's subcategory.
func (c *Catalog) GuessCategory(ctx context.Context, title, description string) (string, error) {
	text := domain.GuessText(title, description)
	if text == "" || c.text == nil {
		return "", nil
	}
	protos, err := c.prototypes(ctx)
	if err != nil {
		return "", err
	}
	vs, err := c.text.Embed(ctx, []string{text})
	if err != nil {
		return "", fmt.Errorf("embed the listing's text: %w", err)
	}
	return domain.GuessCategory(vs[0], protos), nil
}

// prototypes embeds the category texts once per process; a failure is
// tried again on the next call.
func (c *Catalog) prototypes(ctx context.Context) ([]domain.CategoryPrototype, error) {
	select {
	case c.protoLock <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for the category texts: %w", ctx.Err())
	}
	defer func() { <-c.protoLock }()
	if c.protos != nil {
		return c.protos, nil
	}
	tree, err := c.categories.Tree(ctx, locale.Vietnamese)
	if err != nil {
		return nil, fmt.Errorf("read categories: %w", err)
	}
	texts, codes := domain.GuessTexts(tree)
	protos := make([]domain.CategoryPrototype, 0, len(texts))
	for i, chunk := range slices.Collect(slices.Chunk(texts, guessBatch)) {
		vs, embedErr := c.text.Embed(ctx, chunk)
		if embedErr != nil {
			return nil, fmt.Errorf("embed the category texts: %w", embedErr)
		}
		for j, v := range vs {
			protos = append(protos, domain.CategoryPrototype{Code: codes[i*guessBatch+j], Vector: v})
		}
	}
	c.protos = protos
	return protos, nil
}
