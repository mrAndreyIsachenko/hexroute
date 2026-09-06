package ingressagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The configuration is on disk and the runtime has been told to read it. Doing
// only the first would leave the host serving neither version knowably.
func TestApplyingWritesTheConfigurationAndReloadsTheRuntime(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "xray.json")
	marker := filepath.Join(directory, "reloaded")
	generationPath := filepath.Join(directory, "generation")
	applier, err := NewFileApplier(path, generationPath, "/bin/sh", nil)
	if err != nil {
		t.Fatal(err)
	}
	applier.args = []string{"-c", "printf reloaded > " + marker}

	content := []byte(`{"inbounds":[{"port":443}]}`)
	if err := applier.Apply(context.Background(), content, "2026-09-06.1"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil || string(written) != string(content) {
		t.Fatalf("configuration: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != configPermissions {
		t.Fatalf("permissions = %v", info.Mode().Perm())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the runtime was never told to reload")
	}
	// The host now says which version it is running, which is the only way
	// anything can tell a version that works from one that never installed.
	generation, err := os.ReadFile(generationPath)
	if err != nil || strings.TrimSpace(string(generation)) != "2026-09-06.1" {
		t.Fatalf("generation = %q, %v", string(generation), err)
	}
}

// A reload that failed is an apply that failed. Reporting it as success is how
// a host comes to be described as running something it is not.
func TestAFailedReloadIsAFailedApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xray.json")
	applier, err := NewFileApplier(path, filepath.Join(filepath.Dir(path), "generation"),
		"/usr/bin/false", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := applier.Apply(context.Background(), []byte(`{}`), "2026-09-06.1"); !errors.Is(err, ErrApply) {
		t.Fatalf("apply: %v", err)
	}
}

func TestAnApplierNeedsAbsolutePaths(t *testing.T) {
	for _, testCase := range []struct{ path, generation, command string }{
		{path: "xray.json", generation: "/etc/hexroute/generation", command: "/bin/true"},
		{path: "/etc/hexroute/xray.json", generation: "/etc/hexroute/generation", command: "systemctl"},
		{path: "/etc/hexroute/../xray.json", generation: "/etc/hexroute/generation", command: "/bin/true"},
		{path: "/etc/hexroute/xray.json", generation: "generation", command: "/bin/true"},
		{path: "/etc/hexroute/xray.json", generation: "/etc/hexroute/xray.json", command: "/bin/true"},
	} {
		if _, err := NewFileApplier(testCase.path, testCase.generation,
			testCase.command, nil); !errors.Is(err, ErrAgent) {
			t.Fatalf("accepted %q %q %q", testCase.path, testCase.generation, testCase.command)
		}
	}
}
