package connectivityruntime

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const modulePath = "github.com/mrAndreyIsachenko/hexroute"

// connectivityPackages is the whole read model. Every claim below is made
// about all of it, so a new package cannot quietly join with weaker rules.
var connectivityPackages = []string{
	"connectivity", "connectivityaccept", "connectivitycheckpoint",
	"connectivitycollect", "connectivityhost", "connectivityjournal",
	"connectivityreduce", "connectivityruntime", "connectivityview",
}

func importsOf(t *testing.T, directory string) []string {
	t.Helper()
	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, directory, func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", directory, err)
	}
	imports := make([]string, 0)
	for _, parsed := range packages {
		for _, file := range parsed.Files {
			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatalf("%s: %v", directory, err)
				}
				imports = append(imports, path)
			}
		}
	}
	return imports
}

// AdGuard, Codex, Pritunl and Twilight can only be changed by running
// something or opening a socket. The read model can do neither, which is a
// stronger statement than any test that checks their state afterwards: it
// holds for paths nobody thought to check.
func TestReadModelCannotRunOrReachAnything(t *testing.T) {
	forbidden := map[string]string{
		"os/exec":                               "could run a command",
		"net":                                   "could open a socket",
		"net/http":                              "could reach a service",
		modulePath + "/internal/command":        "could run a command",
		modulePath + "/internal/actionlease":    "could mint action authority",
		modulePath + "/internal/actionplan":     "could carry an action plan",
		modulePath + "/internal/routeplan":      "could plan a route change",
		modulePath + "/internal/pritunlplan":    "could plan a Pritunl change",
		modulePath + "/internal/pritunlrescue":  "could restart Pritunl",
		modulePath + "/internal/resumeexecutor": "could execute a resume",
		modulePath + "/internal/credentials":    "could read a credential",
	}
	for _, name := range connectivityPackages {
		directory := filepath.Join("..", name)
		for _, imported := range importsOf(t, directory) {
			if reason, banned := forbidden[imported]; banned {
				t.Fatalf("%s imports %q: it %s", name, imported, reason)
			}
		}
	}
}

// The cloud is telemetry only. The way to guarantee that losing the API,
// PostgreSQL, a worker or the dashboard cannot change what this host concludes
// is that no part of the read model can see any of them: there is no path for
// cloud state to become an input, stale or otherwise.
func TestCloudCannotInfluenceLocalReduction(t *testing.T) {
	forbidden := map[string]string{
		modulePath + "/internal/cloudingest":   "cloud ingest API",
		modulePath + "/internal/cloudhealth":   "cloud health",
		modulePath + "/internal/cloudruntime":  "cloud runtime",
		modulePath + "/internal/cloudincident": "cloud incidents",
		modulePath + "/internal/dashboard":     "dashboard",
		modulePath + "/internal/dashboardauth": "dashboard auth",
		modulePath + "/internal/database":      "database",
		modulePath + "/internal/telemetry":     "telemetry uploader",
		"github.com/jackc/pgx/v5":              "PostgreSQL driver",
	}
	for _, name := range connectivityPackages {
		for _, imported := range importsOf(t, filepath.Join("..", name)) {
			for banned, what := range forbidden {
				if imported == banned || strings.HasPrefix(imported, banned+"/") {
					t.Fatalf("%s imports %q (%s); cloud state must never reach reduction",
						name, imported, what)
				}
			}
		}
	}
}

// The read model must not reach into the policy store either: it consults an
// already-compiled descriptor, and touching the store would let recovery of an
// old snapshot become authorization of an old policy.
func TestReadModelCannotReachThePolicyStore(t *testing.T) {
	forbidden := map[string]struct{}{
		modulePath + "/internal/policystore":     {},
		modulePath + "/internal/policycontrol":   {},
		modulePath + "/internal/policyinstaller": {},
	}
	for _, name := range connectivityPackages {
		for _, imported := range importsOf(t, filepath.Join("..", name)) {
			if _, banned := forbidden[imported]; banned {
				t.Fatalf("%s imports %q", name, imported)
			}
		}
	}
}

// The guard is only worth something if the packages it names still exist.
func TestEveryConnectivityPackageIsCovered(t *testing.T) {
	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatalf("read internal: %v", err)
	}
	covered := make(map[string]struct{}, len(connectivityPackages))
	for _, name := range connectivityPackages {
		covered[name] = struct{}{}
		if info, err := os.Stat(filepath.Join("..", name)); err != nil || !info.IsDir() {
			t.Fatalf("package %s no longer exists; the boundary guard is void", name)
		}
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "connectivity") {
			continue
		}
		if _, listed := covered[entry.Name()]; !listed {
			t.Fatalf("package %s joined the read model without joining its boundary test",
				entry.Name())
		}
	}
}

// Everything the runtime writes must land under the roots it was handed.
func TestRuntimeWritesOnlyWhereItWasPointed(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	beforeOutside := treeOf(t, outside)

	h := openHarness(t, base, true)
	if _, err := h.runtime.Publish(rootFacts(), policy.DomainRoot); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := h.runtime.Publish(userFacts(), policy.DomainUser); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := h.runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick}); err != nil {
		t.Fatalf("tick: %v", err)
	}

	written := treeOf(t, base)
	if len(written) == 0 {
		t.Fatal("the runtime wrote nothing at all")
	}
	for _, path := range written {
		relative, err := filepath.Rel(base, path)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		root := strings.Split(relative, string(filepath.Separator))[0]
		switch root {
		case "readmodel", "root", "user":
		default:
			t.Fatalf("the runtime wrote outside the stores it was given: %s", relative)
		}
	}
	if afterOutside := treeOf(t, outside); len(afterOutside) != len(beforeOutside) {
		t.Fatal("an unrelated directory changed")
	}
}

func treeOf(t *testing.T, root string) []string {
	t.Helper()
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return paths
}

// Root and user evidence stay in separate stores on disk, not only in memory.
func TestDomainEvidenceIsPhysicallySeparate(t *testing.T) {
	base := t.TempDir()
	h := openHarness(t, base, true)
	if _, err := h.runtime.Publish(rootFacts(), policy.DomainRoot); err != nil {
		t.Fatalf("root publish: %v", err)
	}
	if _, err := h.runtime.Publish(userFacts(), policy.DomainUser); err != nil {
		t.Fatalf("user publish: %v", err)
	}

	rootTree := treeOf(t, filepath.Join(base, "root"))
	userTree := treeOf(t, filepath.Join(base, "user"))
	if len(rootTree) == 0 || len(userTree) == 0 {
		t.Fatalf("root=%d user=%d records; both domains should have written",
			len(rootTree), len(userTree))
	}
	// A user component must not appear anywhere in the root store.
	for _, path := range rootTree {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(content), string(connectivity.ComponentUserAccess)) ||
			strings.Contains(string(content), string(connectivity.ComponentSessionExpiry)) {
			t.Fatalf("a user component was written into the root store: %s", path)
		}
	}
}
