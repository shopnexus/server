package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
)

// Listing is a sample listing; its photo is photos/<Key>.jpg.
type Listing struct {
	Key         string               `json:"key"`
	Title       string               `json:"title"`
	Category    string               `json:"category"`
	Price       int64                `json:"price"`
	Condition   string               `json:"condition"`
	Description string               `json:"description"`
	Specs       []catalogdomain.Spec `json:"specs"`
	// PhotoPrompt is what tools/gen_photos.py asked the image model for.
	PhotoPrompt string `json:"photo_prompt"`
}

// Query is one search of the set and the listings it must find.
type Query struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // q, photo or like
	Q    string `json:"q,omitempty"`
	// Photo is a file under photos/, another shot of the listing Of.
	Photo string `json:"photo,omitempty"`
	Of    string `json:"of,omitempty"`
	// Like is the key of the source listing.
	Like   string   `json:"like,omitempty"`
	Expect []string `json:"expect"`
}

// Load reads dir/listings.json and dir/queries.json and checks that they
// agree: unique keys, a field for each kind, expected and source keys that
// name listings, and every photo file present.
func Load(dir string) ([]Listing, []Query, error) {
	var listings []Listing
	var queries []Query
	for name, out := range map[string]any{"listings.json": &listings, "queries.json": &queries} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, nil, fmt.Errorf("read the sample set: %w", err)
		}
		if err = json.Unmarshal(b, out); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	keys := map[string]bool{}
	var errs []error
	photo := func(file string) {
		if _, err := os.Stat(filepath.Join(dir, "photos", file)); err != nil {
			errs = append(errs, fmt.Errorf("photo %s: %w", file, err))
		}
	}
	for i := range listings {
		l := &listings[i]
		if keys[l.Key] {
			errs = append(errs, fmt.Errorf("listing %q twice", l.Key))
		}
		keys[l.Key] = true
		photo(l.Key + ".jpg")
	}
	names := map[string]bool{}
	for _, q := range queries {
		if names[q.Name] {
			errs = append(errs, fmt.Errorf("query %q twice", q.Name))
		}
		names[q.Name] = true
		switch q.Kind {
		case "q":
			if q.Q == "" {
				errs = append(errs, fmt.Errorf("%s: no q", q.Name))
			}
		case "photo":
			if !keys[q.Of] {
				errs = append(errs, fmt.Errorf("%s: of %q is no listing", q.Name, q.Of))
			}
			photo(q.Photo)
		case "like":
			if !keys[q.Like] {
				errs = append(errs, fmt.Errorf("%s: like %q is no listing", q.Name, q.Like))
			}
			if slices.Contains(q.Expect, q.Like) {
				errs = append(errs, fmt.Errorf("%s: like leaves its source out, so it cannot be expected", q.Name))
			}
		default:
			errs = append(errs, fmt.Errorf("%s: kind %q", q.Name, q.Kind))
		}
		if len(q.Expect) == 0 {
			errs = append(errs, fmt.Errorf("%s: expects nothing", q.Name))
		}
		for _, k := range q.Expect {
			if !keys[k] {
				errs = append(errs, fmt.Errorf("%s: expects %q, no listing", q.Name, k))
			}
		}
	}
	return listings, queries, errors.Join(errs...)
}
