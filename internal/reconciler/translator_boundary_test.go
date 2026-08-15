package reconciler

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestProposalTranslatorHasNoIOOrRuntimeAuthority(t *testing.T) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "translator.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{
		"io",
		"net",
		"os",
		"runtime",
		"syscall",
		"time",
		"/internal/credentials",
		"/internal/observe",
		"/internal/policyapproval",
		"/internal/pritunl",
		"/internal/routeplan",
		"/internal/userobserve",
	}
	for _, imported := range parsed.Imports {
		value, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range banned {
			if value == forbidden || strings.Contains(value, forbidden) {
				position := fileSet.Position(imported.Pos())
				t.Fatalf("translator.go:%d imports forbidden authority %q", position.Line, value)
			}
		}
	}
}
