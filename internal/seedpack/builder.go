package seedpack

import (
	"context"

	"github.com/spf13/pflag"
)

// Builder makes a pack from one source; `seedpack build <Name>` runs it. A
// new source is one file with a type that implements Builder, and one line
// in cmd/seedpack's builders list.
type Builder interface {
	// Name is the subcommand, such as "fixtures" or "legacy".
	Name() string
	// Short is a line for the help; Example is shown under it.
	Short() string
	Example() string
	// Pack is the name and version the pack gets unless --name or
	// --version say otherwise.
	Pack() (name, version string)
	// Flags binds the builder's own flags, such as --db; build adds --out,
	// --name, --version, --yes and --json.
	Flags(fs *pflag.FlagSet)
	// Build checks its flags and sources, then adds photos, avatars and
	// lines to w and sets w.Manifest's License, Source and Vectors and
	// w.Readme. build closes w.
	Build(ctx context.Context, w *Writer, p Progress) error
}

// Progress shows long work: the CLI draws a bar per stage, --json draws
// nothing. Its methods are safe for concurrent use.
type Progress interface {
	// Stage starts a stage of total units (rows, or bytes for a download);
	// total 0 is unknown.
	Stage(name string, total int64) Bar
	// Warnf notes something the run skipped or changed.
	Warnf(format string, args ...any)
}

// Bar is one stage's progress.
type Bar interface {
	Add(n int64)
	Done()
}

// Discard is a Progress that shows nothing.
type Discard struct{}

func (Discard) Stage(string, int64) Bar { return discardBar{} }
func (Discard) Warnf(string, ...any)    {}

type discardBar struct{}

func (discardBar) Add(int64) {}
func (discardBar) Done()     {}
