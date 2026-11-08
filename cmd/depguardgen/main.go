// Command depguardgen writes the depguard rules in .golangci.yml that keep
// imports between modules and layers one way:
//
//	depguardgen -root . [-check]
//
// A module is a directory under internal/ that has one of the layer
// packages domain, repository, service, handler and worker. The rules name
// only the packages that exist, so a new module or layer is covered on the
// next run. They replace the lines between the BEGIN and END markers in
// .golangci.yml under -root; the rest of the file stays as it is. With
// -check it writes nothing and fails when the file differs from what it
// would write. make depguard runs it, and its test fails while the file is
// stale.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	beginMarker = "# BEGIN depguardgen"
	endMarker   = "# END depguardgen"
)

// layers are a module's layer packages from the bottom up.
var layers = []string{"domain", "repository", "service", "handler", "worker"}

// denial refuses the import of the given layer packages of any module.
type denial struct {
	layers []string
	desc   string
}

// layerRules says what each layer may not import, in every module. Tests are
// held to the same rule unless skipTests is set.
var layerRules = []struct {
	layer     string
	skipTests bool
	deny      []denial
}{
	{layer: "domain", deny: []denial{
		{[]string{"repository", "service", "handler", "worker"}, "domain imports no layer above it."},
	}},
	{layer: "repository", deny: []denial{
		{[]string{"service", "handler", "worker"}, "repository imports no layer above it."},
	}},
	{layer: "service", deny: []denial{
		{[]string{"handler", "worker"}, "service imports no layer above it."},
	}},
	{layer: "handler", deny: []denial{
		{[]string{"repository"}, "handler reaches data through service."},
		{[]string{"worker"}, "handler imports no worker."},
	}},
	// A worker test may build the module's repositories to set up a case.
	{layer: "worker", skipTests: true, deny: []denial{
		{[]string{"repository", "handler"}, "worker reaches data through service, as a handler does."},
	}},
}

// otherModuleDenied are the layers production code of one module may not
// import from another: it goes through the other module's service.
var otherModuleDenied = []string{"repository", "handler", "worker"}

// serviceCallers lists, for the modules whose service not every other
// module may call, the modules that may.
var serviceCallers = map[string]struct {
	callers []string
	desc    string
}{
	"screen":       {desc: "no module calls screen; it reads the others."},
	"notification": {callers: []string{"screen"}, desc: "only screen calls notification; it hears the others through events."},
}

func main() {
	root := flag.String("root", "", "the repository root, with go.mod, .golangci.yml and internal/")
	check := flag.Bool("check", false, "fail when .golangci.yml is stale instead of writing it")
	flag.Parse()
	err := errors.New("-root is required")
	if *root != "" {
		err = run(*root, *check)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "depguardgen:", err)
		os.Exit(1)
	}
}

func run(root string, check bool) error {
	path := filepath.Join(root, ".golangci.yml")
	current, want, err := generate(root)
	if err != nil {
		return err
	}
	if bytes.Equal(current, want) {
		return nil
	}
	if check {
		return fmt.Errorf("the depguard rules in %s are stale; run make depguard", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, want, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// generate returns .golangci.yml under root as it is and with the rules
// between the markers written anew from the packages under internal/.
func generate(root string) (current, want []byte, err error) {
	modulePath, err := readModulePath(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, nil, err
	}
	modules, err := findModules(filepath.Join(root, "internal"))
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(root, ".golangci.yml")
	current, err = os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}

	lines := strings.SplitAfter(string(current), "\n")
	begin, end := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), beginMarker) && begin < 0 {
			begin = i
		} else if strings.HasPrefix(strings.TrimSpace(l), endMarker) && begin >= 0 && end < 0 {
			end = i
		}
	}
	if begin < 0 || end < 0 {
		return nil, nil, fmt.Errorf("%s needs a %q line and an %q line after it", path, beginMarker, endMarker)
	}
	indent := lines[begin][:len(lines[begin])-len(strings.TrimLeft(lines[begin], " "))]

	var b strings.Builder
	write := func(depth int, format string, args ...any) {
		b.WriteString(indent + strings.Repeat("  ", depth))
		fmt.Fprintf(&b, format, args...)
		b.WriteString("\n")
	}
	deny := func(module, layer, desc string) {
		write(2, "- pkg: %s/internal/%s/%s", modulePath, module, layer)
		write(3, "desc: %s", desc)
	}
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, m := range names {
		write(0, "module %s:", m)
		write(1, "files:")
		write(2, "- %q", "**/internal/"+m+"/**")
		write(2, "- %q", "!$test")
		write(1, "deny:")
		for _, layer := range otherModuleDenied {
			for _, other := range names {
				if other != m && modules[other][layer] {
					deny(other, layer, "another module is reached through its service.")
				}
			}
		}
		for _, other := range names {
			rule, limited := serviceCallers[other]
			if limited && other != m && modules[other]["service"] && !slices.Contains(rule.callers, m) {
				deny(other, "service", rule.desc)
			}
		}
	}
	for _, rule := range layerRules {
		write(0, "%s layer:", rule.layer)
		write(1, "files:")
		write(2, "- %q", "**/internal/*/"+rule.layer+"/**")
		if rule.skipTests {
			write(2, "- %q", "!$test")
		}
		write(1, "deny:")
		for _, d := range rule.deny {
			for _, layer := range d.layers {
				for _, m := range names {
					if modules[m][layer] {
						deny(m, layer, d.desc)
					}
				}
			}
		}
	}

	out := strings.Join(lines[:begin+1], "") + b.String() + strings.Join(lines[end:], "")
	return current, []byte(out), nil
}

// findModules returns each module under dir with the layer packages it has.
// A layer package exists when its directory holds a .go file.
func findModules(dir string) (map[string]map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	modules := map[string]map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, layer := range layers {
			files, err := os.ReadDir(filepath.Join(dir, e.Name(), layer))
			if err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("read %s: %w", filepath.Join(dir, e.Name(), layer), err)
			}
			if slices.ContainsFunc(files, func(f os.DirEntry) bool { return strings.HasSuffix(f.Name(), ".go") }) {
				if modules[e.Name()] == nil {
					modules[e.Name()] = map[string]bool{}
				}
				modules[e.Name()][layer] = true
			}
		}
	}
	if len(modules) == 0 {
		return nil, fmt.Errorf("no module under %s", dir)
	}
	return modules, nil
}

func readModulePath(goMod string) (string, error) {
	data, err := os.ReadFile(goMod)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", goMod, err)
	}
	for l := range strings.Lines(string(data)) {
		if path, ok := strings.CutPrefix(l, "module "); ok {
			return strings.TrimSpace(path), nil
		}
	}
	return "", fmt.Errorf("%s has no module line", goMod)
}
