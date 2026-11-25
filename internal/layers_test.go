// Package internal holds the test that keeps imports between modules and
// layers one way.
package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// layers are a module's layer packages from the bottom up.
// mcp is module agent's MCP server, a handler of another protocol.
var layers = []string{"domain", "repository", "service", "handler", "mcp", "worker"}

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
		{[]string{"repository", "service", "handler", "mcp", "worker"}, "domain imports no layer above it."},
	}},
	{layer: "repository", deny: []denial{
		{[]string{"service", "handler", "mcp", "worker"}, "repository imports no layer above it."},
	}},
	{layer: "service", deny: []denial{
		{[]string{"handler", "mcp", "worker"}, "service imports no layer above it."},
	}},
	{layer: "handler", deny: []denial{
		{[]string{"repository"}, "handler reaches data through service."},
		{[]string{"worker"}, "handler imports no worker."},
	}},
	{layer: "mcp", deny: []denial{
		{[]string{"repository"}, "mcp reaches data through service, as a handler does."},
		{[]string{"handler", "worker"}, "mcp imports no handler or worker."},
	}},
	// A worker test may build the module's repositories to set up a case.
	{layer: "worker", skipTests: true, deny: []denial{
		{[]string{"repository", "handler"}, "worker reaches data through service, as a handler does."},
	}},
}

// otherModuleDenied are the layers production code of one module may not
// import from another: it goes through the other module's service.
var otherModuleDenied = []string{"repository", "handler", "mcp", "worker"}

// serviceCallers lists, for the modules whose service not every other
// module may call, the modules that may.
var serviceCallers = map[string]struct {
	callers []string
	desc    string
}{
	"erasure":      {desc: "no module calls erasure; it calls account and deal."},
	"screen":       {desc: "no module calls screen; it reads the others."},
	"notification": {callers: []string{"screen"}, desc: "only screen calls notification; it hears the others through events."},
}

// listedPackage is what go list says of a package under internal/.
//
//nolint:tagliatelle // go list's own field names.
type listedPackage struct {
	Dir          string   `json:"Dir"`
	ImportPath   string   `json:"ImportPath"`
	GoFiles      []string `json:"GoFiles"`
	CgoFiles     []string `json:"CgoFiles"`
	TestGoFiles  []string `json:"TestGoFiles"`
	XTestGoFiles []string `json:"XTestGoFiles"`
}

// TestLayers checks every import of the files under internal/, as go
// build sees them, against the rules above. A module is a directory under
// internal/ with one of the layer packages.
func TestLayers(t *testing.T) {
	t.Parallel()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := listPackages(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	modules, err := findModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	var base string
	for _, p := range pkgs {
		if p.Dir == dir {
			base = p.ImportPath
		}
	}
	if base == "" {
		t.Fatal("go list did not list internal/ itself")
	}

	// denied is a rule's list of package paths and why each is refused.
	type denied map[string]string
	deny := func(d denied, module, layer, desc string) {
		if modules[module][layer] {
			d[base+"/"+module+"/"+layer] = desc
		}
	}
	moduleRules := map[string]denied{}
	for m := range modules {
		d := denied{}
		for _, layer := range otherModuleDenied {
			for other := range modules {
				if other != m {
					deny(d, other, layer, "another module is reached through its service.")
				}
			}
		}
		for other, rule := range serviceCallers {
			if other != m && !slices.Contains(rule.callers, m) {
				deny(d, other, "service", rule.desc)
			}
		}
		moduleRules[m] = d
	}
	layerDenied := map[string]denied{}
	for _, rule := range layerRules {
		d := denied{}
		for _, dn := range rule.deny {
			for _, layer := range dn.layers {
				for m := range modules {
					deny(d, m, layer, dn.desc)
				}
			}
		}
		layerDenied[rule.layer] = d
	}

	// check reports each import of file that a rule refuses. A rule matches
	// a package and everything under it.
	check := func(file string, imports []string, d denied) {
		for _, imp := range imports {
			for pkg, desc := range d {
				if imp == pkg || strings.HasPrefix(imp, pkg+"/") {
					t.Errorf("%s imports %s: %s", file, imp, desc)
				}
			}
		}
	}
	for _, p := range pkgs {
		rel, err := filepath.Rel(dir, p.Dir)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		prod := slices.Concat(p.GoFiles, p.CgoFiles)
		tests := slices.Concat(p.TestGoFiles, p.XTestGoFiles)
		for _, f := range slices.Concat(prod, tests) {
			path := filepath.Join(p.Dir, f)
			imports, err := fileImports(path)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.ToSlash(filepath.Join("internal", rel, f))
			isTest := strings.HasSuffix(f, "_test.go")
			if d, ok := moduleRules[parts[0]]; ok && !isTest {
				check(name, imports, d)
			}
			if len(parts) < 2 {
				continue
			}
			for _, rule := range layerRules {
				if rule.layer == parts[1] && (!isTest || !rule.skipTests) {
					check(name, imports, layerDenied[rule.layer])
				}
			}
		}
	}
}

// listPackages lists the packages under dir as the default build sees them.
func listPackages(ctx context.Context, dir string) ([]listedPackage, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-e", "-json=Dir,ImportPath,GoFiles,CgoFiles,TestGoFiles,XTestGoFiles", "./...")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w: %s", err, stderr.String())
	}
	var pkgs []listedPackage
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listedPackage
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			return pkgs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decode go list: %w", err)
		}
		pkgs = append(pkgs, p)
	}
}

func fileImports(path string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	imports := make([]string, 0, len(f.Imports))
	for _, spec := range f.Imports {
		imp, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: import %s: %w", path, spec.Path.Value, err)
		}
		imports = append(imports, imp)
	}
	return imports, nil
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
