package policycontrol

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPolicyAuthorizationPackagesHaveNoDataPlaneMutationDependencies(t *testing.T) {
	banned := []string{
		"os/exec",
		"/internal/observe",
		"/internal/pritunlplan",
		"/internal/pritunlrescue",
		"/internal/routeplan",
		"/internal/userobserve",
	}
	for _, directory := range []string{".", "../actionlease", "../policystore", "../policy"} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
				strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			files := token.NewFileSet()
			parsed, err := parser.ParseFile(files, path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imported := range parsed.Imports {
				value, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				for _, forbidden := range banned {
					if value == forbidden || strings.Contains(value, forbidden) {
						position := files.Position(imported.Pos())
						t.Fatalf("%s:%d imports data-plane mutation dependency %q", path, position.Line, value)
					}
				}
			}
		}
	}
}
