package reconciler

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestReconcilerFoundationHasNoProductionMutationDependencies(t *testing.T) {
	banned := []string{
		"net",
		"net/http",
		"os/exec",
		"/internal/cloudruntime",
		"/internal/credentials",
		"/internal/observe",
		"/internal/operator",
		"/internal/policyapproval",
		"/internal/policycontrol",
		"/internal/pritunlplan",
		"/internal/pritunlrescue",
		"/internal/routeplan",
		"/internal/userobserve",
	}
	files := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(files, entry.Name(), nil, parser.ImportsOnly)
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
					t.Fatalf("%s:%d imports forbidden mutation dependency %q", entry.Name(), position.Line, value)
				}
			}
		}
	}
}
