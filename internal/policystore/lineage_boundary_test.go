package policystore

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOnlyLineageRelaxesTheWindowOrTheStaticDigest keeps the relaxation where it
// was argued for. RecoverLineage answers a historical question and may skip the
// two comparisons that are about the present; nothing else in this package may.
//
// A grep of the source would pass for a file that does not compile, so this
// walks the syntax tree and names the function it found.
func TestOnlyLineageRelaxesTheWindowOrTheStaticDigest(t *testing.T) {
	fileSet := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			calls := calledSelectors(function)
			_, binding := calls["VerifyDomainBinding"]
			if !binding {
				continue
			}
			if function.Name.Name != "RecoverLineage" {
				t.Fatalf(
					"%s.%s verifies an approval without its validity window; "+
						"only lineage may, because only lineage asks about the past",
					name, function.Name.Name,
				)
			}
		}
	}
}

// TestRecoverLineageDoesNotUseTheOperationalChecks states the same boundary from
// the other side: the historical path must not reach for the functions that
// compare a generation to the present, or the relaxation is undone silently.
func TestRecoverLineageDoesNotUseTheOperationalChecks(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "lineage.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		"VerifyDomainCandidate",
		"CheckActiveCompatibility",
		"CheckCandidateCompatibility",
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "RecoverLineage" {
			continue
		}
		calls := calledSelectors(function)
		for _, name := range forbidden {
			if _, called := calls[name]; called {
				t.Fatalf("RecoverLineage calls %s, which asks whether a generation may govern now", name)
			}
		}
		return
	}
	t.Fatal("RecoverLineage not found in lineage.go")
}

func calledSelectors(function *ast.FuncDecl) map[string]struct{} {
	calls := make(map[string]struct{})
	ast.Inspect(function, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch callee := call.Fun.(type) {
		case *ast.SelectorExpr:
			calls[callee.Sel.Name] = struct{}{}
		case *ast.Ident:
			calls[callee.Name] = struct{}{}
		}
		return true
	})
	return calls
}
