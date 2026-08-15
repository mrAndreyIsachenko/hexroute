package reconciler

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestShadowPeersDisableCrossDomainWhenUserPeerMissing(t *testing.T) {
	rootStore, err := OpenShadowStore(shadowStoreConfig(policy.DomainRoot, filepath.Join(t.TempDir(), "root-store")))
	if err != nil {
		t.Fatalf("OpenShadowStore(root) error = %v", err)
	}
	rootStatus, err := rootStore.ipcStatus()
	if err != nil {
		t.Fatalf("root ipcStatus() error = %v", err)
	}

	evaluation, err := EvaluateShadowPeers(&rootStatus, nil)
	if err != nil {
		t.Fatalf("EvaluateShadowPeers() error = %v", err)
	}
	if evaluation.CrossDomainAvailable ||
		!evaluation.Root.Available ||
		!evaluation.Root.ObserveOnly ||
		evaluation.Root.Reason != ReasonAccepted ||
		evaluation.User.Available ||
		evaluation.User.ObserveOnly ||
		evaluation.User.Reason != ReasonFreshness {
		t.Fatalf("evaluation = %+v", evaluation)
	}
}

func TestShadowPeersDisableCrossDomainWhenRootPeerMissing(t *testing.T) {
	userStore, err := OpenShadowStore(shadowStoreConfig(policy.DomainUser, filepath.Join(t.TempDir(), "user-store")))
	if err != nil {
		t.Fatalf("OpenShadowStore(user) error = %v", err)
	}
	userStatus, err := userStore.ipcStatus()
	if err != nil {
		t.Fatalf("user ipcStatus() error = %v", err)
	}

	evaluation, err := EvaluateShadowPeers(nil, &userStatus)
	if err != nil {
		t.Fatalf("EvaluateShadowPeers() error = %v", err)
	}
	if evaluation.CrossDomainAvailable ||
		evaluation.Root.Available ||
		evaluation.Root.ObserveOnly ||
		evaluation.Root.Reason != ReasonFreshness ||
		!evaluation.User.Available ||
		!evaluation.User.ObserveOnly ||
		evaluation.User.Reason != ReasonAccepted {
		t.Fatalf("evaluation = %+v", evaluation)
	}
}

func TestShadowPeersEnableCrossDomainOnlyWithBothObserveOnlyPeers(t *testing.T) {
	rootStore, err := OpenShadowStore(shadowStoreConfig(policy.DomainRoot, filepath.Join(t.TempDir(), "root-store")))
	if err != nil {
		t.Fatalf("OpenShadowStore(root) error = %v", err)
	}
	userStore, err := OpenShadowStore(shadowStoreConfig(policy.DomainUser, filepath.Join(t.TempDir(), "user-store")))
	if err != nil {
		t.Fatalf("OpenShadowStore(user) error = %v", err)
	}
	rootStatus, err := rootStore.ipcStatus()
	if err != nil {
		t.Fatalf("root ipcStatus() error = %v", err)
	}
	userStatus, err := userStore.ipcStatus()
	if err != nil {
		t.Fatalf("user ipcStatus() error = %v", err)
	}

	evaluation, err := EvaluateShadowPeers(&rootStatus, &userStatus)
	if err != nil {
		t.Fatalf("EvaluateShadowPeers() error = %v", err)
	}
	if !evaluation.CrossDomainAvailable ||
		!evaluation.Root.Available ||
		!evaluation.Root.ObserveOnly ||
		!evaluation.User.Available ||
		!evaluation.User.ObserveOnly {
		t.Fatalf("evaluation = %+v", evaluation)
	}
}

func TestShadowPeersRejectMismatchedPeerStatus(t *testing.T) {
	rootStore, err := OpenShadowStore(shadowStoreConfig(policy.DomainRoot, filepath.Join(t.TempDir(), "root-store")))
	if err != nil {
		t.Fatalf("OpenShadowStore(root) error = %v", err)
	}
	rootStatus, err := rootStore.ipcStatus()
	if err != nil {
		t.Fatalf("root ipcStatus() error = %v", err)
	}
	if _, err := EvaluateShadowPeers(nil, &rootStatus); !errors.Is(err, ErrShadowStore) {
		t.Fatalf("EvaluateShadowPeers(mismatch) error = %v, want %v", err, ErrShadowStore)
	}
}
