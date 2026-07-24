package sentinel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSentinelProductionSourcesExposeNoProtectedMutationCommand(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("Glob() error: %v", err)
	}
	var production strings.Builder
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error: %v", path, err)
		}
		production.Write(content)
	}
	source := strings.ToLower(production.String())
	for _, forbidden := range []string{
		`"os/exec"`,
		`"/sbin/route"`,
		`"/usr/sbin/route"`,
		`"pritunl"`,
		`"xray"`,
		`"nginx"`,
		`"mtg"`,
		`"adguard"`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("sentinel production source contains forbidden authority %s", forbidden)
		}
	}
	if count := strings.Count(source, ".output("); count != 1 {
		t.Fatalf("sentinel production source has %d runner mutation calls, want 1", count)
	}
}
