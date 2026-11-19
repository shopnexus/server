package seedpack

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Pack is a pack directory opened for reading.
type Pack struct {
	Dir      string
	Manifest Manifest
}

// Open reads dir's manifest and refuses a format this package does not
// know.
func Open(dir string) (*Pack, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("read the manifest: %w", err)
	}
	var m Manifest
	if err = json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if m.Format != Format {
		return nil, fmt.Errorf("%s is format %d; this seedpack reads format %d", ManifestFile, m.Format, Format)
	}
	if m.Name == "" || m.Version == "" {
		return nil, fmt.Errorf("%s needs a name and a version", ManifestFile)
	}
	return &Pack{Dir: dir, Manifest: m}, nil
}

// Path is the absolute path of a file inside the pack.
func (p *Pack) Path(rel string) string { return filepath.Join(p.Dir, filepath.FromSlash(rel)) }

// ReadFile reads a file inside the pack, such as PhotoPath(sha).
func (p *Pack) ReadFile(rel string) ([]byte, error) {
	b, err := os.ReadFile(p.Path(rel))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return b, nil
}

// Has reports whether the pack has a file.
func (p *Pack) Has(rel string) bool {
	_, err := os.Stat(p.Path(rel))
	return err == nil
}

func (p *Pack) Accounts() ([]Account, error) { return readAll[Account](p, AccountsFile, true) }
func (p *Pack) Listings() ([]Listing, error) { return readAll[Listing](p, ListingsFile, true) }

// History is nil for a pack without history.jsonl.
func (p *Pack) History() ([]Sale, error) { return readAll[Sale](p, HistoryFile, false) }

// Follows is nil for a pack without follows.jsonl.
func (p *Pack) Follows() ([]Follow, error) { return readAll[Follow](p, FollowsFile, false) }

// PhotoKeys is nil for a pack without photo vectors.
func (p *Pack) PhotoKeys() ([]PhotoKey, error) { return readAll[PhotoKey](p, PhotoKeysFile, false) }

func readAll[T any](p *Pack, name string, required bool) ([]T, error) {
	var out []T
	err := EachLine(p.Path(name), func(_ int, v T) error {
		out = append(out, v)
		return nil
	})
	if !required && errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return out, err
}

// EachLine decodes each line of a JSONL file as a T, in order, and calls
// fn with its index. Unknown fields are refused, so a typo fails early.
func EachLine[T any](path string, fn func(i int, v T) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = f.Close() }() // read-only
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for i := 0; s.Scan(); i++ {
		var v T
		d := json.NewDecoder(bytes.NewReader(s.Bytes()))
		d.DisallowUnknownFields()
		if err = d.Decode(&v); err != nil {
			return fmt.Errorf("%s line %d: %w", filepath.Base(path), i+1, err)
		}
		if err = fn(i, v); err != nil {
			return err
		}
	}
	if err = s.Err(); err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return nil
}
