package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/schollz/progressbar/v3"

	"github.com/shopnexus/server/internal/seedpack"
)

// ui is how a command talks: bars and warnings on stderr, the summary and
// the result on stdout, or one JSON document with --json.
type ui struct {
	json, yes bool
	out, err  io.Writer
	in        *os.File

	mu       sync.Mutex
	warnings []string
}

func newUI(jsonOut, yes bool) *ui {
	return &ui{json: jsonOut, yes: yes, out: os.Stdout, err: os.Stderr, in: os.Stdin}
}

var (
	accent = lipgloss.Color("#14b8a6")
	muted  = lipgloss.Color("#8a9499")
	label  = lipgloss.NewStyle().Foreground(muted).Width(10)
	value  = lipgloss.NewStyle()
	title  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	note   = lipgloss.NewStyle().Foreground(muted)
)

// labelPack heads the row that names the pack.
const labelPack = "Pack"

// summary prints label, value rows under a title.
func (u *ui) summary(heading string, rows [][2]string) {
	if u.json {
		return
	}
	_, _ = lipgloss.Fprintln(u.out, title.Render(heading))
	for _, r := range rows {
		if r[1] == "" {
			continue
		}
		_, _ = lipgloss.Fprintln(u.out, label.Render(r[0])+value.Render(r[1]))
	}
}

func (u *ui) println(s string) {
	if !u.json {
		_, _ = lipgloss.Fprintln(u.out, s)
	}
}

// confirm asks before a write; --yes answers for it. Without a terminal to
// ask on, a write needs --yes.
func (u *ui) confirm(question string) error {
	if u.yes {
		return nil
	}
	if u.json || !term.IsTerminal(u.in.Fd()) {
		return errors.New("nothing to ask on; pass --yes to write")
	}
	_, _ = lipgloss.Fprint(u.out, title.Render(question)+" [y/N] ")
	line, _ := bufio.NewReader(u.in).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		return errCancelled
	}
	return nil
}

var errCancelled = errors.New("cancelled")

// result prints v as JSON with --json, else calls human.
func (u *ui) result(v any, human func()) error {
	if u.json {
		e := json.NewEncoder(u.out)
		e.SetIndent("", "  ")
		if err := e.Encode(v); err != nil {
			return fmt.Errorf("write the result: %w", err)
		}
		return nil
	}
	human()
	u.flushWarnings()
	return nil
}

func (u *ui) Stage(name string, total int64) seedpack.Bar {
	tty := !u.json && term.IsTerminal(os.Stderr.Fd())
	b := &bar{ui: u, name: name, total: total, tty: tty, start: time.Now()}
	if tty {
		opts := []progressbar.Option{
			progressbar.OptionSetWriter(u.err), progressbar.OptionSetDescription(fmt.Sprintf("%-14s", name)),
			progressbar.OptionSetWidth(28), progressbar.OptionThrottle(100 * time.Millisecond),
			progressbar.OptionShowCount(), progressbar.OptionSetPredictTime(true), progressbar.OptionFullWidth(),
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionSetTheme(progressbar.Theme{Saucer: "[cyan]━[reset]", SaucerPadding: "─", BarStart: " ", BarEnd: " "}),
			progressbar.OptionOnCompletion(func() { _, _ = fmt.Fprintln(u.err) }),
		}
		if strings.HasPrefix(name, "download") {
			opts = append(opts, progressbar.OptionShowBytes(true))
		}
		if total <= 0 {
			total = -1
		}
		b.pb = progressbar.NewOptions64(total, opts...)
	}
	return b
}

func (u *ui) Warnf(format string, args ...any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.warnings = append(u.warnings, fmt.Sprintf(format, args...))
}

// flushWarnings prints the warnings kept so far, the first 20 of them.
func (u *ui) flushWarnings() {
	u.mu.Lock()
	ws := u.warnings
	u.warnings = nil
	u.mu.Unlock()
	if u.json || len(ws) == 0 {
		return
	}
	for i, w := range ws {
		if i == 20 {
			_, _ = lipgloss.Fprintln(u.err, note.Render(fmt.Sprintf("  … and %d more", len(ws)-20)))
			break
		}
		_, _ = lipgloss.Fprintln(u.err, note.Render("  ! "+w))
	}
}

// bar is one stage: a progress bar on a terminal, one line when it ends
// otherwise.
type bar struct {
	ui    *ui
	pb    *progressbar.ProgressBar
	name  string
	total int64
	done  int64
	tty   bool
	start time.Time
	mu    sync.Mutex
}

func (b *bar) Add(n int64) {
	b.mu.Lock()
	b.done += n
	b.mu.Unlock()
	if b.pb != nil {
		_ = b.pb.Add64(n)
	}
}

func (b *bar) Done() {
	if b.pb != nil {
		_ = b.pb.Finish()
		return
	}
	if !b.ui.json {
		b.mu.Lock()
		defer b.mu.Unlock()
		_, _ = fmt.Fprintf(b.ui.err, "%-14s %d done in %s\n", b.name, b.done, time.Since(b.start).Round(time.Millisecond))
	}
}

// count writes n with thousands separators.
func count(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}
