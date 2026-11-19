package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shopnexus/server/internal/seedpack"
	"github.com/shopnexus/server/internal/seedpack/loader"
)

func loadCmd() *cobra.Command {
	var (
		pack, photos  string
		o             loader.Options
		dryRun, yes   bool
		jsonOut       bool
		only          []string
		categories    []string
		provinces     []string
		historyRatio  float64
		workers       int
		embedEvery    time.Duration
		vectors       string
		limit, seller int
	)
	cmd := &cobra.Command{
		Use:   "load [pack]",
		Short: "Write a pack's people, listings, sales and follows into the dev stack",
		Long: `load writes a pack through the server's own entities and repositories: people
sign up with their phone, photos become processed uploads, listings are
posted and published, sold items go through a deal and a delivered COD order.
It shows what it will write and asks first. Rows get IDs from the pack's name
and their keys, so a second load skips what is there.

The pack is a directory, a URL of a .tar (downloaded once into
~/.cache/shopnexus/seedpacks), or a name: demo is cmd/devseed/data/demo.`,
		Example: `  seedpack load demo
  seedpack load vn-catalog-10k --limit 2000 --category phones,laptop --province 79
  seedpack load https://packs.example/vn-catalog-2026.11.1.tar --vectors recompute --yes
  seedpack load demo --dry-run --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			u := newUI(jsonOut, yes)
			ref, err := packArg(pack, args)
			if err != nil {
				return err
			}
			if photos != "full" && photos != "first" {
				return fmt.Errorf("--photos %q; want full or first", photos)
			}
			o = loader.Options{
				Vectors: vectors, Only: only, Limit: limit, Categories: categories, Provinces: provinces, Sellers: seller,
				FirstPhotoOnly: photos == "first", HistoryRatio: historyRatio, Workers: workers, EmbedEvery: embedEvery,
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
			plan, err := l.Plan(ctx, p, o)
			if err != nil {
				return err
			}
			m := plan.Pack
			writes := fmt.Sprintf("%s people · %s listings · %s photos · %s sold with %s reviews · %s follows",
				count(plan.People.Total), count(plan.Listings.Total), count(plan.Photos), count(plan.Sold.Total),
				count(plan.Reviews), count(plan.Follows))
			there := ""
			if n := plan.People.There + plan.Listings.There + plan.Sold.There; n > 0 {
				there = fmt.Sprintf("%s people · %s listings · %s sold are there already and are skipped",
					count(plan.People.There), count(plan.Listings.There), count(plan.Sold.There))
			}
			dropped := ""
			if plan.ReviewsDropped > 0 {
				dropped = count(plan.ReviewsDropped) + " buyer reviews and replies: today's schema keeps only the buyer's review of the seller"
			}
			u.summary("seedpack load", [][2]string{
				{labelPack, fmt.Sprintf("%s %s (%s)", m.Name, m.Version, m.License)},
				{"Source", m.Source},
				{"Target", s.target()},
				{"Writes", writes},
				{"There", there},
				{"Vectors", "text: " + plan.TextVectors + " · photo: " + plan.PhotoVectors},
				{"Dropped", dropped},
				{"Skipped", strings.Join(plan.Skipped, " · ")},
			})
			if dryRun {
				return u.result(map[string]any{"plan": plan}, func() { u.println(note.Render("dry run: nothing written")) })
			}
			if err = u.confirm("Continue?"); err != nil {
				return err
			}
			start := time.Now()
			res, err := l.Load(ctx, plan, u)
			if err != nil {
				u.flushWarnings()
				return err
			}
			return u.result(map[string]any{"plan": plan, "result": res}, func() {
				u.summary("Loaded in "+time.Since(start).Round(time.Second).String(), [][2]string{
					{"People", fmt.Sprintf("%s written, %s skipped", count(res.People.Written), count(res.People.Skipped))},
					{"Listings", fmt.Sprintf("%s written, %s skipped", count(res.Listings.Written), count(res.Listings.Skipped))},
					{"Sold", fmt.Sprintf("%s written with %s reviews, %s skipped", count(res.Sold.Written), count(res.Reviews), count(res.Sold.Skipped))},
					{"Follows", count(res.Follows)},
					{"Objects", count(int(res.Objects))},
					{"Vectors", fmt.Sprintf("%s text · %s photo from the pack · %s listings sent to ListingEmbed",
						count(res.Vectors.Text), count(res.Vectors.Photo), count(res.Vectors.Refreshes))},
				})
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&pack, "pack", "", "the pack: a directory, a URL or a name")
	f.StringVar(&vectors, "vectors", loader.VectorsPack, "pack, recompute or none: write the pack's vectors where they fit the stack, leave them to the server, or skip")
	f.StringSliceVar(&only, "only", nil, "run only these stages: accounts, listings, history, follows")
	f.IntVar(&limit, "limit", 0, "keep the first N listings that pass the filters")
	f.StringSliceVar(&categories, "category", nil, "keep listings under these categories, roots or leaves, such as phones,laptop")
	f.StringSliceVar(&provinces, "province", nil, "keep listings in these province codes, such as 79,01")
	f.IntVar(&seller, "sellers", 0, "keep the listings of the first N sellers")
	f.StringVar(&photos, "photos", "full", "full, or first: one photo a listing")
	f.Float64Var(&historyRatio, "history-ratio", 1, "the share of sold items to load, 0 to 1")
	f.IntVar(&workers, "workers", 8, "rows written at once")
	f.DurationVar(&embedEvery, "embed-every", 200*time.Millisecond, "the time between two listings sent to ListingEmbed")
	f.BoolVar(&dryRun, "dry-run", false, "show what would be written and stop")
	f.BoolVarP(&yes, "yes", "y", false, "write without asking")
	f.BoolVar(&jsonOut, "json", false, "print the plan and the result as JSON, without bars")
	cmd.Flags().SortFlags = false
	return cmd
}
