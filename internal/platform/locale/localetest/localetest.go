// Package localetest checks that the keys a package's code writes are in its
// catalog.
package localetest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/platform/locale"
)

// Keys fails t for each string literal the non-test Go files in dir pass to
// one of calls (a function or method name → the argument index of the key)
// that c lacks in any language. A key built at run time is not checked.
func Keys(t *testing.T, c locale.Catalog, dir string, calls map[string]int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	check := func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			name = fn.Name
		case *ast.SelectorExpr:
			name = fn.Sel.Name
		default:
			return true
		}
		i, ok := calls[name]
		if !ok || len(call.Args) <= i {
			return true
		}
		lit, ok := call.Args[i].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		key, _ := strconv.Unquote(lit.Value)
		found++
		for _, tag := range locale.Supported {
			if _, ok := c[tag][key]; !ok {
				t.Errorf("%s.json lacks %q", tag, key)
			}
		}
		return true
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, check)
	}
	if found == 0 {
		t.Fatalf("no keys found in %s", dir)
	}
}
