package ingressagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The configuration is on disk and the runtime has been told to read it. Doing
// only the first would leave the host serving neither version knowably.
func TestApplyingWritesTheConfigurationAndReloadsTheRuntime(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "xray.json")
	marker := filepath.Join(directory, "reloaded")
	applier, err := NewFileApplier(path, "/bin/sh", nil)
	if err != nil {
		t.Fatal(err)
	}
	applier.args = []string{"-c", "printf reloaded > " + marker}

	content := []byte(`{"inbounds":[{"port":443}]}`)
	if err := applier.Apply(context.Background(), content); err != nil {
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
}

// A reload that failed is an apply that failed. Reporting it as success is how
// a host comes to be described as running something it is not.
func TestAFailedReloadIsAFailedApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xray.json")
	applier, err := NewFileApplier(path, "/usr/bin/false", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := applier.Apply(context.Background(), []byte(`{}`)); !errors.Is(err, ErrApply) {
		t.Fatalf("apply: %v", err)
	}
}

func TestAnApplierNeedsAbsolutePaths(t *testing.T) {
	for _, testCase := range []struct{ path, command string }{
		{path: "xray.json", command: "/bin/true"},
		{path: "/etc/hexroute/xray.json", command: "systemctl"},
		{path: "/etc/hexroute/../xray.json", command: "/bin/true"},
	} {
		if _, err := NewFileApplier(testCase.path, testCase.command, nil); !errors.Is(err, ErrAgent) {
			t.Fatalf("accepted %q %q", testCase.path, testCase.command)
		}
	}
}
