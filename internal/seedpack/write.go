package seedpack

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/imaging"
)

// Writer builds a pack in a new directory. A builder adds photos and
// avatars, then the lines, and Close writes pack.json with the counts.
// Every method is safe for concurrent use.
type Writer struct {
	// Manifest is what Close writes; a builder sets License, Source and
	// Vectors, Close the format and the counts.
	Manifest Manifest
	// Readme is README.md; empty writes none.
	Readme string

	dir     string
	mu      sync.Mutex
	files   map[string]*jsonlFile
	photos  map[string]bool
	avatars map[string]bool
}

type jsonlFile struct {
	f *os.File
	w *bufio.Writer
	n int
}

// Create starts a pack named name at version in dir, which must not exist
// or be empty.
func Create(dir, name, version string) (*Writer, error) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%s is not empty", dir)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("make %s: %w", dir, err)
	}
	return &Writer{
		Manifest: Manifest{Format: Format, Name: name, Version: version},
		dir:      dir, files: map[string]*jsonlFile{}, photos: map[string]bool{}, avatars: map[string]bool{},
	}, nil
}

// Dir is where the pack is written.
func (w *Writer) Dir() string { return w.dir }

// ProcessPhoto makes a listing photo and its thumbnail from an original
// JPEG, PNG or WebP, as the media service does.
func ProcessPhoto(original []byte) (photo, thumb []byte, err error) {
	photo, thumb, err = imaging.Process(bytes.NewReader(original), http.DetectContentType(original), domain.ShapePhoto)
	if err != nil {
		return nil, nil, fmt.Errorf("process the photo: %w", err)
	}
	return photo, thumb, nil
}

// ProcessAvatar makes an avatar's square from an original photo.
func ProcessAvatar(original []byte) ([]byte, error) {
	square, _, err := imaging.Process(bytes.NewReader(original), http.DetectContentType(original), domain.ShapeSquare)
	if err != nil {
		return nil, fmt.Errorf("process the avatar: %w", err)
	}
	return square, nil
}

// AddPhoto stores a processed photo and its thumbnail once and returns the
// photo's SHA-256, which a Listing names.
func (w *Writer) AddPhoto(photo, thumb []byte) (string, error) {
	if len(photo) == 0 || len(thumb) == 0 {
		return "", errors.New("a photo needs its thumbnail")
	}
	sha := hash(photo)
	w.mu.Lock()
	seen := w.photos[sha]
	w.photos[sha] = true
	w.mu.Unlock()
	if seen {
		return sha, nil
	}
	if err := w.put(PhotoPath(sha), photo); err != nil {
		return "", err
	}
	return sha, w.put(ThumbPath(sha), thumb)
}

// AddAvatar stores a square once and returns its SHA-256, which an Account
// names.
func (w *Writer) AddAvatar(square []byte) (string, error) {
	sha := hash(square)
	w.mu.Lock()
	seen := w.avatars[sha]
	w.avatars[sha] = true
	w.mu.Unlock()
	if seen {
		return sha, nil
	}
	return sha, w.put(AvatarPath(sha), square)
}

func (w *Writer) put(rel string, b []byte) error {
	path := filepath.Join(w.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("make %s: %w", filepath.Dir(rel), err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	return nil
}

// Account checks a person against the server's rules and appends it;
// Listing, Sale and Follow do the same for theirs.
func (w *Writer) Account(a Account) error {
	if err := CheckAccount(a); err != nil {
		return err
	}
	if a.Avatar != "" && !w.has(w.avatars, a.Avatar) {
		return fmt.Errorf("account %s: avatar %s was not added", a.Key, a.Avatar)
	}
	return w.line(AccountsFile, a)
}

func (w *Writer) Listing(l Listing) error {
	if err := w.checkListing(l); err != nil {
		return err
	}
	return w.line(ListingsFile, l)
}

func (w *Writer) Sale(s Sale) error {
	if err := CheckSale(s); err != nil {
		return err
	}
	if err := w.checkListing(s.Listing); err != nil {
		return err
	}
	s.Listing.Status = StatusSold
	return w.line(HistoryFile, s)
}

func (w *Writer) Follow(f Follow) error {
	if f.Follower == "" || f.Followee == "" || f.Follower == f.Followee {
		return fmt.Errorf("follow %s → %s: two different people", f.Follower, f.Followee)
	}
	return w.line(FollowsFile, f)
}

func (w *Writer) checkListing(l Listing) error {
	if err := CheckListing(l); err != nil {
		return err
	}
	for _, sha := range l.Photos {
		if !w.has(w.photos, sha) {
			return fmt.Errorf("listing %s: photo %s was not added", l.Key, sha)
		}
	}
	return nil
}

func (w *Writer) has(set map[string]bool, sha string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return set[sha]
}

func (w *Writer) line(name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode a line of %s: %w", name, err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	f := w.files[name]
	if f == nil {
		file, createErr := os.Create(filepath.Join(w.dir, name))
		if createErr != nil {
			return fmt.Errorf("create %s: %w", name, createErr)
		}
		f = &jsonlFile{f: file, w: bufio.NewWriter(file)}
		w.files[name] = f
	}
	f.n++
	_, _ = f.w.Write(b)
	if err = f.w.WriteByte('\n'); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// Close flushes the lines and writes README.md and pack.json, whose counts
// it fills in. Reviews counts the reviews of every sale.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, name := range []string{AccountsFile, ListingsFile} {
		if w.files[name] == nil {
			return fmt.Errorf("the pack has no %s", name)
		}
	}
	for name, f := range w.files {
		if err := f.w.Flush(); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		if err := f.f.Close(); err != nil {
			return fmt.Errorf("close %s: %w", name, err)
		}
	}
	count := func(name string) int {
		if f := w.files[name]; f != nil {
			return f.n
		}
		return 0
	}
	reviews := 0
	if w.files[HistoryFile] != nil {
		if err := EachLine(filepath.Join(w.dir, HistoryFile), func(_ int, s Sale) error {
			reviews += len(s.Reviews)
			return nil
		}); err != nil {
			return err
		}
	}
	m := w.Manifest
	m.Format = Format
	m.Counts = Counts{
		Accounts: count(AccountsFile), Listings: count(ListingsFile), Photos: len(w.photos), Avatars: len(w.avatars),
		Sold: count(HistoryFile), Reviews: reviews, Follows: count(FollowsFile),
	}
	if w.Readme != "" {
		if err := os.WriteFile(filepath.Join(w.dir, ReadmeFile), []byte(w.Readme), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", ReadmeFile, err)
		}
	}
	return WriteManifest(w.dir, m)
}

// WriteManifest writes m as dir's pack.json.
func WriteManifest(dir string, m Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the manifest: %w", err)
	}
	if err = os.WriteFile(filepath.Join(dir, ManifestFile), append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", ManifestFile, err)
	}
	return nil
}

func hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
