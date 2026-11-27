package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/provider/chotot"
	"github.com/shopnexus/server/internal/provider/llm"
)

// FillAd is UC-LST-04, step 5: it writes the draft of a seller's Chợ Tốt
// listing, with the condition and the specs the listing states. It keeps
// no read and relays nothing.
func (a *Assist) FillAd(ctx context.Context, ad chotot.Ad) (AdFill, error) {
	started := time.Now()
	modelCtx, cancel := context.WithTimeout(ctx, a.d.CallTimeout)
	defer cancel()
	raw, err := a.d.Model.Stream(modelCtx, llm.Request{
		System: adSystemPrompt, User: adUserPrompt(ad), SchemaName: domain.ReplySchemaName, Schema: domain.ReplySchema,
	}, func(string) {})
	if err != nil {
		return AdFill{}, fmt.Errorf("ask the model: %w", err)
	}
	rep, err := domain.ParseReply(raw)
	if err != nil {
		return AdFill{}, fmt.Errorf("read the model's answer: %w", err)
	}
	d, removed, err := domain.AdDraft(rep, ad.Price)
	if noItem, ok := errors.AsType[domain.NoItemError](err); ok {
		a.d.Log.InfoContext(ctx, "ad fill found no item", "list_id", ad.ListID, "chotot_category", ad.CategoryName)
		return AdFill{NoItem: noItem.Reason, Guessed: []string{}}, nil
	}
	if err != nil {
		return AdFill{}, fmt.Errorf("clean the answer: %w", err)
	}
	fields := make([]catalogdomain.Spec, len(ad.Params))
	for i, p := range ad.Params {
		fields[i] = catalogdomain.Spec{Label: p.Label, Value: p.Value}
	}
	guessed := []string{}
	if c, ok := domain.AdCondition(fields); ok {
		d.Condition = c
	} else {
		guessed = append(guessed, "condition")
	}
	d.CategoryCode = a.d.guessCategory(ctx, uuid.UUID{}, d)
	if d.CategoryCode != "" {
		keyed, keyErr := a.d.Catalog.KeySpecs(ctx, d.CategoryCode, domain.AdSpecs(fields))
		if keyErr != nil {
			return AdFill{}, fmt.Errorf("key the specs: %w", keyErr)
		}
		d.Specs = slices.DeleteFunc(keyed, func(s catalogdomain.Spec) bool { return s.Key == "" })
	}
	// No word of the seller's: no subject, body, title or description.
	a.d.Log.InfoContext(ctx, "ad filled", "prompt_version", AdPromptVersion, "list_id", ad.ListID,
		"body_chars", utf8.RuneCountInString(ad.Body), "chotot_category", ad.CategoryName, "category", d.CategoryCode,
		"specs", len(d.Specs), "guessed", guessed, "contact_removed", removed, "ms", time.Since(started).Milliseconds())
	return AdFill{Draft: d, ContactRemoved: removed, Guessed: guessed}, nil
}
