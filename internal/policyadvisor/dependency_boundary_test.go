package policyadvisor

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestAdvisorHasNoApprovalInstallationOrRuntimeAuthority(t *testing.T) {
	banned := []string{
		"/internal/ipc",
		"/internal/policyapproval",
		"/internal/policycli",
		"/internal/policycontrol",
		"/internal/policystore",
		"/internal/rootdaemon",
		"/internal/userdaemon",
		"net",
		"os/exec",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		files := token.NewFileSet()
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
					t.Fatalf("%s imports forbidden authority %q", entry.Name(), value)
				}
			}
		}
	}
}
