package connectivityreduce

import (
	"go/parser"
	"go/token"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

// Reduction is pure by contract, and a contract that is only a comment decays.
// This states it as a property of the package's imports: the reducer cannot
// read a clock, a file, an environment variable or a socket because nothing
// that could is reachable from it.
func TestReducerCannotReachTheOutsideWorld(t *testing.T) {
	permitted := map[string]struct{}{
		"errors": {}, "fmt": {}, "sort": {},
		"github.com/mrAndreyIsachenko/hexroute/internal/connectivity":       {},
		"github.com/mrAndreyIsachenko/hexroute/internal/connectivityaccept": {},
		"github.com/mrAndreyIsachenko/hexroute/internal/control":            {},
		"github.com/mrAndreyIsachenko/hexroute/internal/policy":             {},
		"github.com/mrAndreyIsachenko/hexroute/internal/safety":             {},
	}

	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(packages) == 0 {
		t.Fatal("no package sources were parsed")
	}
	for _, parsed := range packages {
		for name, file := range parsed.Files {
			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if _, allowed := permitted[path]; !allowed {
					t.Fatalf("%s imports %q; reduction must stay pure", name, path)
				}
			}
		}
	}
}
