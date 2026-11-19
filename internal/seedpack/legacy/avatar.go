package legacy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/shopnexus/server/internal/seedpack"
)

// avatar is one square of the pool, added to the pack.
type avatar struct {
	sha, kind, gender string
}

// loadAvatars adds every square of --avatars to the pack. index.json lists
// each file with its kind (person, pet, scenery, cartoon, object) and, for
// a person, a gender of f, m or u (unclear).
func (b *Builder) loadAvatars(w *seedpack.Writer) ([]avatar, error) {
	if b.avatars == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(filepath.Join(b.avatars, "index.json"))
	if err != nil {
		return nil, fmt.Errorf("read the avatar index: %w", err)
	}
	type entry struct {
		File   string `json:"file"`
		Kind   string `json:"kind"`
		Gender string `json:"gender"`
	}
	var index []entry
	if err = json.Unmarshal(raw, &index); err != nil {
		var wrapped struct {
			Avatars []entry `json:"avatars"`
		}
		if json.Unmarshal(raw, &wrapped) != nil {
			return nil, fmt.Errorf("the avatar index: %w", err)
		}
		index = wrapped.Avatars
	}
	out := make([]avatar, 0, len(index))
	for _, e := range index {
		img, readErr := os.ReadFile(filepath.Join(b.avatars, e.File))
		if readErr != nil {
			return nil, fmt.Errorf("avatar %s: %w", e.File, readErr)
		}
		square, procErr := seedpack.ProcessAvatar(img)
		if procErr != nil {
			return nil, fmt.Errorf("avatar %s: %w", e.File, procErr)
		}
		sha, addErr := w.AddAvatar(square)
		if addErr != nil {
			return nil, addErr
		}
		g := e.Gender
		if g != "f" && g != "m" {
			g = "" // unclear fits anyone
		}
		out = append(out, avatar{sha: sha, kind: e.Kind, gender: g})
	}
	return out, nil
}

// pickAvatar draws a square: a face of the person's gender a little over
// half the time, else a pet, a view, a doodle or a thing.
func pickAvatar(avatars []avatar, gender string, x, kindX float64) string {
	want := func(a avatar) bool { return a.kind == "person" && (a.gender == gender || a.gender == "") }
	if kindX >= 0.55 {
		want = func(a avatar) bool { return a.kind != "person" }
	}
	var list []avatar
	for _, a := range avatars {
		if want(a) {
			list = append(list, a)
		}
	}
	if len(list) == 0 {
		list = slices.Clone(avatars)
	}
	return list[int(x*float64(len(list)))].sha
}
