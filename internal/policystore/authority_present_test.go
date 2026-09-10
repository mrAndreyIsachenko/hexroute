package policystore

import (
	"os"
	"path/filepath"
	"testing"
)

// A store holding an authority beside a configuration that cannot read it is
// the state this machine was left in on 2026-09-10, and nothing said so. The
// question has to be answerable without a pinned key, because a runtime that
// has one is not the one asking.
func TestAnAuthorityInTheStoreIsVisibleWithoutReadingIt(t *testing.T) {
	store := t.TempDir()
	if AuthorityPresent(store) {
		t.Fatal("an empty store reported an authority")
	}
	state := filepath.Join(store, stateDirectory)
	if err := os.MkdirAll(state, DirectoryMode); err != nil {
		t.Fatalf("state directory: %v", err)
	}
	if AuthorityPresent(store) {
		t.Fatal("a store with no active pointer reported an authority")
	}
	// The content is deliberately not a valid pointer. Whoever asks this has no
	// key to validate it with, and reporting only what validates would report
	// nothing in exactly the case worth reporting.
	pointer := filepath.Join(state, activePointerFilename)
	if err := os.WriteFile(pointer, []byte("{}"), GenerationFileMode); err != nil {
		t.Fatalf("active pointer: %v", err)
	}
	if !AuthorityPresent(store) {
		t.Fatal("a store holding an active pointer reported nothing")
	}
}

func TestWhatIsNotAStoreHoldsNoAuthority(t *testing.T) {
	if AuthorityPresent("") {
		t.Fatal("an empty path reported an authority")
	}
	store := t.TempDir()
	// A directory where the pointer should be is not a pointer.
	if err := os.MkdirAll(
		filepath.Join(store, stateDirectory, activePointerFilename),
		DirectoryMode,
	); err != nil {
		t.Fatalf("directory: %v", err)
	}
	if AuthorityPresent(store) {
		t.Fatal("a directory in the pointer's place reported an authority")
	}
}
