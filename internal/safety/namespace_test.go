package safety

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestObserveOnlyLifecyclePreservesProductionBaseline(t *testing.T) {
	root := t.TempDir()
	production := filepath.Join(root, "Library", "Application Support", "twilight")
	candidate := filepath.Join(root, "Library", "Application Support", "Hexroute", "observe")
	productionLabel := "com.twilight.supervisor"
	candidateLabel := "com.hexroute.observe"

	if err := os.MkdirAll(production, 0o700); err != nil {
		t.Fatalf("MkdirAll(production) error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(production, "runtime"), []byte("known-good"), 0o600); err != nil {
		t.Fatalf("WriteFile(production) error: %v", err)
	}

	boundary := ProductionBoundary{
		Paths:  []string{production},
		Labels: []string{productionLabel},
	}
	before := snapshotTree(t, production)
	loadedLabels := map[string]bool{productionLabel: true}

	install := LifecycleOperation{
		Name:       "observe-only-install",
		WritePaths: []string{candidate},
		Labels:     []string{candidateLabel},
	}
	applyFixtureOperation(t, install, boundary, loadedLabels, func() {
		if err := os.MkdirAll(candidate, 0o700); err != nil {
			t.Fatalf("MkdirAll(candidate) error: %v", err)
		}
		if err := os.WriteFile(filepath.Join(candidate, "state"), []byte("observe-only"), 0o600); err != nil {
			t.Fatalf("WriteFile(candidate) error: %v", err)
		}
		loadedLabels[candidateLabel] = true
	})

	execute := LifecycleOperation{Name: "observe-only-execution"}
	applyFixtureOperation(t, execute, boundary, loadedLabels, func() {})

	uninstall := LifecycleOperation{
		Name:        "observe-only-uninstall",
		RemovePaths: []string{candidate},
		Labels:      []string{candidateLabel},
	}
	applyFixtureOperation(t, uninstall, boundary, loadedLabels, func() {
		if err := os.RemoveAll(candidate); err != nil {
			t.Fatalf("RemoveAll(candidate) error: %v", err)
		}
		delete(loadedLabels, candidateLabel)
	})

	after := snapshotTree(t, production)
	if fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("production snapshot changed:\nbefore=%v\nafter=%v", before, after)
	}
	if !loadedLabels[productionLabel] {
		t.Fatal("production launchd label was removed")
	}
}

func TestLifecycleBoundaryRejectsProductionOverlap(t *testing.T) {
	boundary := ProductionBoundary{
		Paths: []string{
			"/Library/Application Support/twilight/supervisor",
		},
		Labels: []string{
			"com.twilight.supervisor",
			"com.twilight.pritunl-otp-watchdog",
		},
	}

	tests := []struct {
		name      string
		operation LifecycleOperation
		want      error
	}{
		{
			name: "write exact production path",
			operation: LifecycleOperation{
				Name:       "install",
				WritePaths: []string{"/Library/Application Support/twilight/supervisor"},
			},
			want: ErrProtectedPath,
		},
		{
			name: "write parent of production path",
			operation: LifecycleOperation{
				Name:       "install",
				WritePaths: []string{"/Library/Application Support/twilight"},
			},
			want: ErrProtectedPath,
		},
		{
			name: "remove child of production path",
			operation: LifecycleOperation{
				Name:        "uninstall",
				RemovePaths: []string{"/Library/Application Support/twilight/supervisor/config"},
			},
			want: ErrProtectedPath,
		},
		{
			name: "reuse production label",
			operation: LifecycleOperation{
				Name:   "install",
				Labels: []string{"COM.TWILIGHT.SUPERVISOR"},
			},
			want: ErrProtectedLabel,
		},
		{
			name: "relative path",
			operation: LifecycleOperation{
				Name:       "install",
				WritePaths: []string{"Library/Application Support/Hexroute"},
			},
			want: ErrInvalidLifecyclePath,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateLifecycleOperation(test.operation, boundary)
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidateLifecycleOperation() error = %v, want %v", err, test.want)
			}
		})
	}
}

func applyFixtureOperation(
	t *testing.T,
	operation LifecycleOperation,
	boundary ProductionBoundary,
	labels map[string]bool,
	apply func(),
) {
	t.Helper()

	if err := ValidateLifecycleOperation(operation, boundary); err != nil {
		t.Fatalf("ValidateLifecycleOperation(%q) error: %v", operation.Name, err)
	}
	for _, label := range operation.Labels {
		if label == "" {
			t.Fatalf("operation %q contains an empty label", operation.Name)
		}
	}
	apply()
}

func snapshotTree(t *testing.T, root string) []string {
	t.Helper()

	var snapshot []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		hash := "-"
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			hash = fmt.Sprintf("%x", sha256.Sum256(content))
		}
		snapshot = append(snapshot, fmt.Sprintf(
			"%s|%s|%d|%s",
			relative,
			info.Mode().String(),
			info.Size(),
			hash,
		))
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%q) error: %v", root, err)
	}
	sort.Strings(snapshot)
	return snapshot
}
