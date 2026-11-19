package seedpack

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// RepoPacks is where packs that ship with the server repository live,
// relative to its root: demo is RepoPacks/demo.
const RepoPacks = "cmd/devseed/data"

// CacheDir is where downloaded packs are unpacked:
// ~/.cache/shopnexus/seedpacks/<name of the tar without .tar>.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find the cache: %w", err)
	}
	return filepath.Join(base, "shopnexus", "seedpacks"), nil
}

// Resolve finds a pack by directory, by URL of a .tar with a .sha256
// beside it (unpacked once into CacheDir), or by name in RepoPacks and the
// cache.
func Resolve(ctx context.Context, ref string, prog Progress) (string, error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return download(ctx, ref, prog)
	}
	if isPack(ref) {
		return ref, nil
	}
	if strings.ContainsAny(ref, `/\`) {
		return "", fmt.Errorf("%s has no %s", ref, ManifestFile)
	}
	if dir := filepath.Join(RepoPacks, ref); isPack(dir) {
		return dir, nil
	}
	cache, err := CacheDir()
	if err != nil {
		return "", err
	}
	if dir := filepath.Join(cache, ref); isPack(dir) {
		return dir, nil
	}
	matches, _ := filepath.Glob(filepath.Join(cache, ref+"-*"))
	for _, m := range slices.Backward(matches) {
		if isPack(m) {
			return m, nil
		}
	}
	return "", fmt.Errorf("no pack %q: not a directory, a URL, %s/%s or in %s", ref, RepoPacks, ref, cache)
}

func isPack(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ManifestFile))
	return err == nil && !info.IsDir()
}

// download fetches url's .sha256, then the tar unless the cache holds it
// unpacked under that sum, checks the sum and unpacks it.
func download(ctx context.Context, url string, prog Progress) (string, error) {
	cache, err := CacheDir()
	if err != nil {
		return "", err
	}
	name := strings.TrimSuffix(path.Base(url), ".tar")
	dir := filepath.Join(cache, name)
	want, err := fetchSum(ctx, url+".sha256")
	if err != nil {
		return "", err
	}
	if have, _ := os.ReadFile(filepath.Join(dir, ".sha256")); strings.TrimSpace(string(have)) == want && isPack(dir) {
		return dir, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", url, res.Status)
	}
	tmp := dir + ".part"
	_ = os.RemoveAll(tmp)
	bar := prog.Stage("download "+name, res.ContentLength)
	h := sha256.New()
	err = untar(io.TeeReader(&countingReader{r: res.Body, bar: bar}, h), tmp)
	bar.Done()
	if err != nil {
		return "", err
	}
	_, _ = io.Copy(h, res.Body) // tar padding after the last file
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("%s has SHA-256 %s, its .sha256 says %s", url, got, want)
	}
	if err = os.WriteFile(filepath.Join(tmp, ".sha256"), []byte(want+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write the sum: %w", err)
	}
	_ = os.RemoveAll(dir)
	if err = os.Rename(tmp, dir); err != nil {
		return "", fmt.Errorf("move the pack into the cache: %w", err)
	}
	if !isPack(dir) {
		return "", fmt.Errorf("%s holds no %s at its top", url, ManifestFile)
	}
	return dir, nil
}

func fetchSum(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", url, res.Status)
	}
	line, err := bufio.NewReader(io.LimitReader(res.Body, 1024)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read %s: %w", url, err)
	}
	sum, _, _ := strings.Cut(strings.TrimSpace(line), " ")
	if !isSHA(sum) {
		return "", fmt.Errorf("%s holds no SHA-256", url)
	}
	return sum, nil
}

// untar unpacks regular files and directories into dir, refusing a path
// that leaves it.
func untar(r io.Reader, dir string) error {
	t := tar.NewReader(r)
	for {
		h, err := t.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the tar: %w", err)
		}
		rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(h.Name, "./")))
		if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if rel == "." {
				continue
			}
			return fmt.Errorf("the tar has a path outside the pack: %s", h.Name)
		}
		target := filepath.Join(dir, rel)
		switch h.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(target, 0o750); err != nil {
				return fmt.Errorf("make %s: %w", rel, err)
			}
		case tar.TypeReg:
			if err = os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return fmt.Errorf("make %s: %w", filepath.Dir(rel), err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return fmt.Errorf("create %s: %w", rel, err)
			}
			_, err = io.Copy(f, t) //nolint:gosec // the tar's SHA-256 is checked after
			if closeErr := f.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				return fmt.Errorf("write %s: %w", rel, err)
			}
		default: // links and devices have no place in a pack
		}
	}
}

type countingReader struct {
	r   io.Reader
	bar Bar
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.bar.Add(int64(n))
	return n, err //nolint:wrapcheck // a reader passes its source's io.EOF as it is
}
