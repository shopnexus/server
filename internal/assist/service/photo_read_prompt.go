package service

import (
	"strings"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

// PhotoPromptVersion names the photo read's prompt and
// domain.PhotoAnswerSchema; a change to either bumps it.
const PhotoPromptVersion = 1

// photoUserPrompt goes with the photo.
const photoUserPrompt = "The seller's photo is attached below.\n"

// photoSystemPrompt is the model's instructions with the subcategories.
func photoSystemPrompt(cats []catalogdomain.Category) string {
	var b strings.Builder
	b.WriteString(`You read one photo a seller took to sell an item on ShopNexus, a Vietnamese second-hand marketplace, and
fill in their listing from it. The photo is all you get: nothing the seller wrote or said comes with it.

Fill these fields, in Vietnamese with diacritics unless a field says otherwise:
- is_item: true when the photo shows one item for sale. false when it shows no item, several unrelated items, only a
  person, a place, a screenshot or a document, or anything that cannot be sold as one listing; the other fields then
  do not matter.
- title: at most 70 characters: the item, its brand and model when the photo shows them, then its colour, as in
  "iPhone 11 Trắng". Only what the photo shows: no capacity, battery health or size it cannot show, no price, no
  emoji, no "bán gấp", "giá rẻ", "chính hãng" or "zin".
- category_code: one code from the list below, and only from it; the most specific one that fits. When no
  subcategory fits, the code ending in ".other" under the right group.
- condition: new (sealed, or boxed and plainly unused), like_new (no wear a buyer would notice in the photo) or used.
  When the photo cannot tell, used.
- specs: label and value pairs a buyer compares, with Vietnamese labels ("Màu": "Trắng", "Hãng": "Apple"), only what
  the photo shows clearly. At most 20, each label and value at most 50 characters.
- description: at most 300 characters, one or two plain sentences of what the photo shows: the item, its colour and
  what comes with it in the frame (a box, a charger, a case). Never name a mark, scratch, dent, crack, stain or fault,
  even when the photo shows one: the seller says that. Nothing the photo cannot show:
  no age, use, repairs, battery health, warranty or why they sell. No praise.
- estimate: what this item in this condition sells for second-hand in Vietnam now, in whole đồng, as low, median and
  high. null when you cannot tell the item or its model well enough to price it.

Categories (code — group › name):
`)
	writeCategories(&b, cats)
	return b.String()
}
