package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shopnexus/server/internal/seedpack"
	"github.com/shopnexus/server/internal/seedpack/loader"
)

func wipeCmd() *cobra.Command {
	var (
		pack                 string
		dryRun, yes, jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "wipe [pack]",
		Short: "Delete one pack's people and everything they made from the dev stack",
		Long: `wipe finds the pack's people by the IDs load gave them and deletes them with
every row that refers to them, following the schema's foreign keys: their
listings, uploads, deals, orders, reviews, chats and follows, including what
they share with people of other packs or of the app. Then it deletes their
uploads' objects. Other people's own rows stay.`,
		Example: `  seedpack wipe demo
  seedpack wipe vn-catalog-10k --dry-run`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			u := newUI(jsonOut, yes)
			ref, err := packArg(pack, args)
			if err != nil {
				return err
			}
			s, err := openStack(ctx)
			if err != nil {
				return err
			}
			defer s.close()
			dir, err := seedpack.Resolve(ctx, ref, u)
			if err != nil {
				return err
			}
			p, err := seedpack.Open(dir)
			if err != nil {
				return err
			}
			l := loader.New(s.deps())
			plan, err := l.PlanWipe(ctx, p)
			if err != nil {
				return err
			}
			u.summary("seedpack wipe", [][2]string{
				{labelPack, fmt.Sprintf("%s %s", plan.Pack.Name, plan.Pack.Version)},
				{"Target", s.target()},
				{"Deletes", fmt.Sprintf("%s people · %s listings · %s uploads and their objects",
					count(plan.People), count(plan.Listings), count(plan.Uploads))},
				{"With them", fmt.Sprintf("%s deals · %s chat messages, with anyone", count(plan.Deals), count(plan.Messages))},
			})
			if plan.People == 0 {
				return u.result(map[string]any{"plan": plan}, func() { u.println(note.Render("nothing of this pack is in the stack")) })
			}
			if dryRun {
				return u.result(map[string]any{"plan": plan}, func() { u.println(note.Render("dry run: nothing deleted")) })
			}
			if err = u.confirm("Delete?"); err != nil {
				return err
			}
			res, err := l.Wipe(ctx, plan, u)
			if err != nil {
				return err
			}
			return u.result(map[string]any{"plan": plan, "result": res}, func() {
				u.summary("Wiped", [][2]string{{"Rows", count(int(res.Rows))}, {"Objects", count(res.Objects)}})
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&pack, "pack", "", "the pack: a directory, a URL or a name")
	f.BoolVar(&dryRun, "dry-run", false, "show what would be deleted and stop")
	f.BoolVarP(&yes, "yes", "y", false, "delete without asking")
	f.BoolVar(&jsonOut, "json", false, "print the plan and the result as JSON, without bars")
	return cmd
}

func verifyCmd() *cobra.Command {
	var (
		pack             string
		noHashes, asJSON bool
		workers          int
	)
	cmd := &cobra.Command{
		Use:   "verify [pack]",
		Short: "Check a pack's files, lines and vectors offline",
		Long: `verify checks a pack without a stack: the manifest's counts, every line
against the server's rules and the people and files it names, the SHA-256
of every photo and avatar, and the shapes of the vector files. load checks
the categories and communes against the stack.`,
		Example: `  seedpack verify cmd/devseed/data/demo
  seedpack verify vn-catalog --no-hashes`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			u := newUI(asJSON, true)
			ref, err := packArg(pack, args)
			if err != nil {
				return err
			}
			dir, err := seedpack.Resolve(ctx, ref, u)
			if err != nil {
				return err
			}
			p, err := seedpack.Open(dir)
			if err != nil {
				return err
			}
			r, err := seedpack.Verify(ctx, p, !noHashes, workers, u)
			if err != nil {
				return err
			}
			err = u.result(r, func() {
				m := p.Manifest
				u.summary("seedpack verify", [][2]string{
					{labelPack, fmt.Sprintf("%s %s (%s) in %s", m.Name, m.Version, m.License, dir)},
					{"Holds", fmt.Sprintf("%s people · %s listings · %s photos · %s avatars · %s sold with %s reviews · %s follows",
						count(r.Counts.Accounts), count(r.Counts.Listings), count(r.Counts.Photos), count(r.Counts.Avatars),
						count(r.Counts.Sold), count(r.Counts.Reviews), count(r.Counts.Follows))},
				})
				for _, w := range r.Warnings {
					u.println(note.Render("  ~ " + w))
				}
				for _, pr := range r.Problems {
					u.println("  ✗ " + pr)
				}
				if r.OK() {
					u.println(title.Render("OK"))
				}
			})
			if err == nil && !r.OK() {
				err = fmt.Errorf("%d problems", len(r.Problems))
			}
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&pack, "pack", "", "the pack: a directory, a URL or a name")
	f.BoolVar(&noHashes, "no-hashes", false, "check that files exist without hashing them")
	f.IntVar(&workers, "workers", 8, "files hashed at once")
	f.BoolVar(&asJSON, "json", false, "print the report as JSON")
	return cmd
}
