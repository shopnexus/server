package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/shopnexus/server/internal/seedpack"
)

func buildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build <source>",
		Short: "Make a pack from a source",
		Long: `build makes a pack from one source into a new directory, then verifies it.
Each source is a subcommand with its own flags.`,
	}
	for _, b := range builders() {
		cmd.AddCommand(builderCmd(b))
	}
	return cmd
}

func builderCmd(b seedpack.Builder) *cobra.Command {
	var (
		out, name, version string
		jsonOut            bool
	)
	defName, defVersion := b.Pack()
	cmd := &cobra.Command{
		Use:     b.Name(),
		Short:   b.Short(),
		Example: b.Example(),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			u := newUI(jsonOut, true)
			if out == "" {
				return errors.New("--out is required: the directory to write the pack to")
			}
			w, err := seedpack.Create(out, name, version)
			if err != nil {
				return err
			}
			if err = b.Build(ctx, w, u); err != nil {
				u.flushWarnings()
				return fmt.Errorf("build %s: %w", b.Name(), err)
			}
			if err = w.Close(); err != nil {
				return err
			}
			p, err := seedpack.Open(out)
			if err != nil {
				return err
			}
			r, err := seedpack.Verify(ctx, p, true, 8, u)
			if err != nil {
				return err
			}
			size := dirSize(out)
			err = u.result(map[string]any{"manifest": p.Manifest, "verify": r, "bytes": size}, func() {
				c := p.Manifest.Counts
				u.summary("Built "+p.Manifest.Name+" "+p.Manifest.Version, [][2]string{
					{"In", fmt.Sprintf("%s, %.1f MB", out, float64(size)/(1<<20))},
					{"Holds", fmt.Sprintf("%s people · %s listings · %s photos · %s avatars · %s sold with %s reviews · %s follows",
						count(c.Accounts), count(c.Listings), count(c.Photos), count(c.Avatars), count(c.Sold), count(c.Reviews), count(c.Follows))},
				})
				for _, pr := range r.Problems {
					u.println("  ✗ " + pr)
				}
			})
			if err == nil && !r.OK() {
				err = fmt.Errorf("the pack fails verify: %d problems", len(r.Problems))
			}
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&out, "out", "", "a new or empty directory to write the pack to (required)")
	f.StringVar(&name, "name", defName, "the pack's name, which its rows' IDs derive from")
	f.StringVar(&version, "version", defVersion, "the pack's version")
	f.BoolVar(&jsonOut, "json", false, "print the manifest and the check as JSON, without bars")
	b.Flags(f)
	return cmd
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
