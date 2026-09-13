package spool

import (
	"path/filepath"
	"testing"
)

// Opening walks the directory once.
//
// It walked it twice: recovery listed it with a stat for every file to check
// the total against the bound, and opening listed it again for one number —
// the highest sequence — which that listing already held.
//
// Measured on the machine on 2026-09-13, opening the user journal cost 10.2
// seconds of a window in which the daemon observes nothing, while listing its
// directory costs 360 milliseconds. The listings are not the whole of that
// difference, but they are the half that is paid twice.
func TestOpeningWalksTheDirectoryOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	store, err := Open(path, OwnerRoot, testOptions(64*1024*1024))
	if err != nil {
		t.Fatalf("open spool: %v", err)
	}
	for index := 1; index <= 60; index++ {
		if _, err := store.Append(costEvent(t, index)); err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
	}

	reopened, err := Open(path, OwnerRoot, testOptions(64*1024*1024))
	if err != nil {
		t.Fatalf("reopen spool: %v", err)
	}
	if reopened.scans != 1 {
		t.Fatalf("opening took %d listings of the directory, want 1",
			reopened.scans)
	}

	// And the listing it kept is the one it opened with, so the first append
	// does not pay for a third.
	if _, err := reopened.Append(costEvent(t, 61)); err != nil {
		t.Fatalf("append after reopening: %v", err)
	}
	if reopened.scans != 1 {
		t.Fatalf("opening and one append took %d listings, want 1",
			reopened.scans)
	}
}

// A spool holding more than its bound admits is refused at open.
//
// The check has been there since the beginning and nothing held it: removing it
// left every test passing. It was found by mutating the line beside the one
// this change touches, which is the argument for mutating the neighbourhood
// rather than only the edit.
func TestOpeningRefusesASpoolOverItsBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	store, err := Open(path, OwnerRoot, testOptions(64*1024*1024))
	if err != nil {
		t.Fatalf("open spool: %v", err)
	}
	for index := 1; index <= 40; index++ {
		if _, err := store.Append(costEvent(t, index)); err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
	}
	stored, err := store.Size()
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if stored == 0 {
		t.Fatal("the spool holds nothing; this proves nothing about a bound")
	}

	// Reopened under a bound it already exceeds: the directory cannot be
	// reconciled with the rule the spool is meant to keep, and a spool whose
	// shape is wrong cannot be reasoned about at all.
	if _, err := Open(path, OwnerRoot, testOptions(stored/2)); err == nil {
		t.Fatalf("a spool holding %d bytes opened under a bound of %d",
			stored, stored/2)
	}
}
