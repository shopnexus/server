package seedpack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"golang.org/x/sync/errgroup"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

// Report is what Verify found: Problems make a pack unfit to load,
// Warnings do not.
type Report struct {
	Problems []string `json:"problems"`
	Warnings []string `json:"warnings"`
	Counts   Counts   `json:"counts"`
}

// OK reports a pack without problems.
func (r Report) OK() bool { return len(r.Problems) == 0 }

// maxListed is how many problems of one run a report keeps.
const maxListed = 50

// Verify checks a pack offline: counts, lines against the server's rules,
// references, file hashes when hashes is set, and vector shapes. Categories
// and communes need a stack; load checks those.
func Verify(ctx context.Context, p *Pack, hashes bool, workers int, prog Progress) (Report, error) {
	var r Report
	var mu sync.Mutex
	problem := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		if len(r.Problems) < maxListed {
			r.Problems = append(r.Problems, fmt.Sprintf(format, args...))
		} else if len(r.Problems) == maxListed {
			r.Problems = append(r.Problems, "and more; fix these first")
		}
	}

	accounts, err := p.Accounts()
	if err != nil {
		return r, err
	}
	listings, err := p.Listings()
	if err != nil {
		return r, err
	}
	sales, err := p.History()
	if err != nil {
		return r, err
	}
	follows, err := p.Follows()
	if err != nil {
		return r, err
	}

	bar := prog.Stage("lines", int64(len(accounts)+len(listings)+len(sales)+len(follows)))
	people, phones := map[string]bool{}, map[string]string{}
	avatars := map[string]bool{}
	for i := range accounts {
		a := &accounts[i]
		bar.Add(1)
		if err := CheckAccount(*a); err != nil {
			problem("%v", err)
		}
		if people[a.Key] {
			problem("account %s twice", a.Key)
		}
		people[a.Key] = true
		if other, dup := phones[a.Phone]; dup {
			problem("accounts %s and %s share phone %s", other, a.Key, a.Phone)
		}
		phones[a.Phone] = a.Key
		if a.Avatar != "" {
			avatars[a.Avatar] = true
		}
	}
	photos, keys, onSale := map[string]bool{}, map[string]bool{}, map[string]int{}
	textLegacy := p.Manifest.Vectors.Text != nil && p.Manifest.Vectors.Text.Origin == OriginLegacy
	unhashed := 0
	listing := func(l Listing, sold bool) {
		if err := CheckListing(l); err != nil {
			problem("%v", err)
		}
		if keys[l.Key] {
			problem("listing %s twice", l.Key)
		}
		keys[l.Key] = true
		if !people[l.Seller] {
			problem("listing %s: no account %s", l.Key, l.Seller)
		}
		switch {
		case sold && l.Status != StatusSold:
			problem("sale %s: the listing's status is %q, want sold", l.Key, l.Status)
		case !sold && l.Status != "" && l.Status != StatusActive && l.Status != StatusHidden:
			problem("listing %s: status %q, want active or hidden", l.Key, l.Status)
		case !sold:
			onSale[l.Seller]++
		default: // a sold listing in history.jsonl
		}
		for _, sha := range l.Photos {
			photos[sha] = true
		}
		if p.Manifest.Vectors.Text != nil && !textLegacy && l.TextSHA256 == "" {
			unhashed++
		}
	}
	for i := range listings {
		l := &listings[i]
		bar.Add(1)
		listing(*l, false)
	}
	for i := range sales {
		s := &sales[i]
		bar.Add(1)
		listing(s.Listing, true)
		if err := CheckSale(*s); err != nil {
			problem("%v", err)
		}
		if !people[s.Buyer] {
			problem("sale %s: no account %s", s.Listing.Key, s.Buyer)
		}
		r.Counts.Reviews += len(s.Reviews)
	}
	pairs := map[[2]string]bool{}
	for _, f := range follows {
		bar.Add(1)
		if !people[f.Follower] || !people[f.Followee] || f.Follower == f.Followee {
			problem("follow %s → %s: two people of the pack", f.Follower, f.Followee)
		}
		if pairs[[2]string{f.Follower, f.Followee}] {
			problem("follow %s → %s twice", f.Follower, f.Followee)
		}
		pairs[[2]string{f.Follower, f.Followee}] = true
	}
	bar.Done()
	for seller, n := range onSale {
		if n > catalogdomain.MaxOnSale {
			problem("%s has %d listings on sale or hidden; the server allows %d", seller, n, catalogdomain.MaxOnSale)
		}
	}
	if unhashed > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%d listings have no text_sha256; their text vectors are left to recompute", unhashed))
	}

	r.Counts.Accounts, r.Counts.Listings, r.Counts.Photos = len(accounts), len(listings), len(photos)
	r.Counts.Avatars, r.Counts.Sold, r.Counts.Follows = len(avatars), len(sales), len(follows)
	if r.Counts != p.Manifest.Counts {
		problem("the manifest counts %+v, the files hold %+v", p.Manifest.Counts, r.Counts)
	}

	// Files: every photo has its thumbnail, and each file is named by its
	// SHA-256.
	files := make([]string, 0, 2*len(photos)+len(avatars))
	for sha := range photos {
		files = append(files, PhotoPath(sha), ThumbPath(sha))
	}
	for sha := range avatars {
		files = append(files, AvatarPath(sha))
	}
	sort.Strings(files)
	fbar := prog.Stage("files", int64(len(files)))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(max(workers, 1))
	for _, rel := range files {
		g.Go(func() error {
			defer fbar.Add(1)
			if gctx.Err() != nil {
				return gctx.Err()
			}
			if !hashes {
				if !p.Has(rel) {
					problem("%s is missing", rel)
				}
				return nil
			}
			sum, err := fileSHA(p.Path(rel))
			if err != nil {
				problem("%s: %v", rel, err)
				return nil
			}
			// A thumbnail is named by its photo, not by its own bytes.
			if rel[:len(ThumbsDir)] != ThumbsDir && sum+".jpg" != rel[len(rel)-len(sum)-4:] {
				problem("%s has SHA-256 %s", rel, sum)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return r, fmt.Errorf("check files: %w", err)
	}
	fbar.Done()

	// Vectors: the shapes match the lines.
	if v := p.Manifest.Vectors.Text; v != nil {
		checkMatrix(p, TextVectors, len(listings)+len(sales), TextDims, problem)
	}
	if v := p.Manifest.Vectors.Photo; v != nil {
		pk, err := p.PhotoKeys()
		if err != nil {
			problem("%v", err)
		}
		for i, k := range pk {
			if !photos[k.Photo] || !keys[k.Listing] {
				problem("%s line %d names a photo or listing the pack lacks", PhotoKeysFile, i+1)
				break
			}
		}
		checkMatrix(p, PhotoVectors, len(pk), PhotoDims, problem)
	}
	return r, nil
}

func checkMatrix(p *Pack, rel string, rows, cols int, problem func(string, ...any)) {
	m, err := OpenMatrix(p.Path(rel), false)
	if err != nil {
		problem("%v", err)
		return
	}
	defer func() { _ = m.Close() }() // read-only
	if m.Rows != rows || m.Cols != cols {
		problem("%s is %d×%d, want %d×%d", rel, m.Rows, m.Cols, rows, cols)
	}
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
