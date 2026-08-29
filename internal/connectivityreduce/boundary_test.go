package connectivityreduce

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	modulePath      = "github.com/mrAndreyIsachenko/hexroute"
	readModelImport = modulePath + "/internal/connectivityreduce"
)

// mutationPackages are the packages that can actually change the host or mint
// the authority to do so.
var mutationPackages = []string{
	modulePath + "/internal/actionlease",
	modulePath + "/internal/actionplan",
	modulePath + "/internal/command",
	modulePath + "/internal/resumeexecutor",
	modulePath + "/internal/routeplan",
	modulePath + "/internal/pritunlplan",
}

// packageImports returns the non-test imports of every package in the module,
// keyed by directory.
func packageImports(t *testing.T) map[string][]string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	imports := make(map[string][]string)
	fileSet := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		directory := filepath.Dir(path)
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			imports[directory] = append(imports[directory], value)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(imports) == 0 {
		t.Fatal("no packages were scanned")
	}
	return imports
}

func contains(list []string, value string) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

// The guard below is only worth anything if the packages it names still
// exist. A rename would otherwise leave it passing forever.
func TestMutationPackagesStillExist(t *testing.T) {
	for _, mutation := range mutationPackages {
		relative := strings.TrimPrefix(mutation, modulePath+"/")
		path := filepath.Join("..", "..", filepath.FromSlash(relative))
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Fatalf("mutation package %s no longer exists; the boundary guard is void",
				mutation)
		}
	}
}

// A proposal is not executable, and the way to keep it that way is to make
// sure nothing that holds one can also reach the code that mutates the host.
//
// Nothing imports the read model yet, so today this passes by having nothing
// to check. It becomes load-bearing when the daemon integration lands, which
// is exactly when it needs to already be here.
func TestNoPackageCanBothHoldAProposalAndMutate(t *testing.T) {
	for directory, imports := range packageImports(t) {
		if !contains(imports, readModelImport) {
			continue
		}
		for _, mutation := range mutationPackages {
			if contains(imports, mutation) {
				t.Fatalf("%s imports both the connectivity read model and %s",
					directory, mutation)
			}
		}
	}
}

// The IPC surface must not even be able to name a proposal.
func TestIPCCannotCarryAProposal(t *testing.T) {
	imports := packageImports(t)
	root, err := filepath.Abs(filepath.Join("..", "..", "internal", "ipc"))
	if err != nil {
		t.Fatalf("resolve ipc: %v", err)
	}
	if contains(imports[root], readModelImport) {
		t.Fatal("the IPC package imports the connectivity read model")
	}
}

// The IPC action set is closed. No member of it may offer execution, and a new
// one that did would have to pass here first.
func TestNoIPCActionOffersExecution(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "ipc", "protocol.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read protocol: %v", err)
	}
	forbidden := []string{
		"proposal", "reconciliation", "action_lease", "lease",
		"execute", "apply_plan", "mutate",
	}
	for _, line := range strings.Split(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Action") || !strings.Contains(trimmed, "=") {
			continue
		}
		value := strings.ToLower(trimmed)
		for _, needle := range forbidden {
			if strings.Contains(value, `"`) && strings.Contains(value, needle) {
				t.Fatalf("IPC action %q offers execution", trimmed)
			}
		}
	}
}

// The read model exports nothing that hands out authority.
func TestReadModelExportsNoLeaseOrExecutor(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	forbidden := []string{"Lease", "Execute", "Apply", "Mutate", "Commit"}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		source, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		for _, line := range strings.Split(string(source), "\n") {
			if !strings.HasPrefix(line, "func ") {
				continue
			}
			for _, needle := range forbidden {
				if strings.Contains(line, needle) {
					t.Fatalf("%s exports %q", entry.Name(), strings.TrimSpace(line))
				}
			}
		}
	}
}
