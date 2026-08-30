package reconciler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestShadowStoresUseDisjointDomainLocalPathsAndOwnership(t *testing.T) {
	base := t.TempDir()
	rootConfig := shadowStoreConfig(policy.DomainRoot, filepath.Join(base, "root-store"))
	userConfig := shadowStoreConfig(policy.DomainUser, filepath.Join(base, "user-store"))
	if err := ValidateDisjointShadowStores(rootConfig, userConfig); err != nil {
		t.Fatalf("ValidateDisjointShadowStores() error = %v", err)
	}

	rootStore, err := OpenShadowStore(rootConfig)
	if err != nil {
		t.Fatalf("OpenShadowStore(root) error = %v", err)
	}
	userStore, err := OpenShadowStore(userConfig)
	if err != nil {
		t.Fatalf("OpenShadowStore(user) error = %v", err)
	}
	if rootStore.Domain() != policy.DomainRoot ||
		userStore.Domain() != policy.DomainUser ||
		rootStore.RootPath() == userStore.RootPath() ||
		rootStore.PeerUID() != uint32(os.Getuid()) ||
		userStore.PeerUID() != uint32(os.Getuid()) {
		t.Fatalf("stores root=%+v user=%+v", rootStore, userStore)
	}

	rootBinding := attemptBinding()
	if _, err := rootStore.AppendPending(rootBinding, ReasonAccepted); err != nil {
		t.Fatalf("root AppendPending() error = %v", err)
	}
	if _, err := rootStore.CompareAndSwap(rootBinding, AttemptPending, AttemptClaimed, ReasonAccepted); err != nil {
		t.Fatalf("root claim error = %v", err)
	}
	latest, exists, err := rootStore.Latest(rootBinding.ActionID)
	if err != nil || !exists || latest.Binding.Domain != policy.DomainRoot ||
		latest.Attempt.State != AttemptClaimed {
		t.Fatalf("root latest=%+v exists=%v error=%v", latest, exists, err)
	}

	userBinding := attemptBinding()
	userBinding.Domain = policy.DomainUser
	userBinding.ActionID = metadata.UUID("99999999-9999-4999-8999-999999999999")
	if _, err := userStore.AppendPending(userBinding, ReasonAccepted); err != nil {
		t.Fatalf("user AppendPending() error = %v", err)
	}
	if _, err := userStore.AppendPending(rootBinding, ReasonAccepted); !errors.Is(err, ErrShadowStore) {
		t.Fatalf("cross-domain append error = %v, want %v", err, ErrShadowStore)
	}
	if _, exists, err := userStore.Latest(rootBinding.ActionID); err != nil || exists {
		t.Fatalf("user store saw root action exists=%v error=%v", exists, err)
	}
}

func TestShadowStoresRejectOverlappingPaths(t *testing.T) {
	base := t.TempDir()
	rootConfig := shadowStoreConfig(policy.DomainRoot, filepath.Join(base, "root-store"))
	userConfig := shadowStoreConfig(policy.DomainUser, filepath.Join(base, "root-store", "user-store"))
	if err := ValidateDisjointShadowStores(rootConfig, userConfig); !errors.Is(err, ErrShadowStore) {
		t.Fatalf("ValidateDisjointShadowStores() error = %v, want %v", err, ErrShadowStore)
	}
}

func TestShadowIPCAuthenticatesPeerAndReturnsTypedDomainStatus(t *testing.T) {
	store, err := OpenShadowStore(shadowStoreConfig(policy.DomainRoot, filepath.Join(t.TempDir(), "root-store")))
	if err != nil {
		t.Fatalf("OpenShadowStore() error = %v", err)
	}
	path, stop := startShadowTestServer(t, uint32(os.Getuid()), store, &shadowReporter{})
	defer stop()

	response, err := (ipc.Client{Path: path}).Do(context.Background(), ipc.Request{
		Version:                ipc.ProtocolVersion,
		RequestID:              "shadow-status",
		Action:                 ipc.ActionReconcilerShadowStatus,
		ReconcilerShadowStatus: &ipc.ReconcilerShadowStatusRequest{},
	})
	if err != nil {
		t.Fatalf("Client.Do() error = %v", err)
	}
	status := response.ReconcilerShadowStatus
	if !response.OK ||
		status == nil ||
		status.Domain != policy.DomainRoot ||
		status.Role != ipc.RoleRoot ||
		!status.ShadowIPC ||
		!status.SyntheticOnly ||
		status.ProposalTranslation ||
		status.ExecutionIPC ||
		len(status.CapabilityIDs) != len(DefaultSyntheticRegistry().IDs()) {
		t.Fatalf("response = %+v", response)
	}
}

func TestShadowIPCRejectsUnauthorizedPeerBeforeDispatch(t *testing.T) {
	store, err := OpenShadowStore(shadowStoreConfig(policy.DomainUser, filepath.Join(t.TempDir(), "user-store")))
	if err != nil {
		t.Fatalf("OpenShadowStore() error = %v", err)
	}
	reporter := &shadowReporter{}
	path, stop := startShadowTestServer(t, uint32(os.Getuid()+1), store, reporter)
	defer stop()

	_, err = (ipc.Client{Path: path, Timeout: time.Second}).Do(context.Background(), ipc.Request{
		Version:                ipc.ProtocolVersion,
		RequestID:              "shadow-unauthorized",
		Action:                 ipc.ActionReconcilerShadowStatus,
		ReconcilerShadowStatus: &ipc.ReconcilerShadowStatusRequest{},
	})
	if err == nil {
		t.Fatal("unauthorized client received a response")
	}
	if !reporter.seen(ipc.ErrUnauthorizedPeer, time.Second) {
		t.Fatalf("unauthorized peer was not reported; reports=%v", reporter.reports())
	}
}

func shadowStoreConfig(domain policy.Domain, path string) ShadowStoreConfig {
	return ShadowStoreConfig{
		Domain:      domain,
		RootPath:    path,
		ExpectedUID: uint32(os.Getuid()),
		ExpectedGID: uint32(os.Getgid()),
		PeerUID:     uint32(os.Getuid()),
		Registry:    DefaultSyntheticRegistry(),
	}
}

// shortTempBase returns a temporary directory shallow enough to hold a Unix
// socket path.
//
// t.TempDir() cannot be used here: on macOS it lives under /var/folders and
// the resulting path runs past the 104-byte sun_path limit. /private/tmp is
// the short base there and does not exist elsewhere, so the platform decides
// rather than the test assuming one.
func shortTempBase() string {
	if info, err := os.Stat("/private/tmp"); err == nil && info.IsDir() {
		return "/private/tmp"
	}
	return "/tmp"
}

func startShadowTestServer(
	t *testing.T,
	allowedUID uint32,
	handler ipc.Handler,
	reporter ipc.RejectionReporter,
) (string, func()) {
	t.Helper()
	directory, err := os.MkdirTemp(shortTempBase(), "hexroute-shadow-ipc-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "shadow.sock")
	server, err := ipc.Listen(path, uint32(os.Getuid()), allowedUID, handler, reporter)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx)
	}()
	return path, func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Serve() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Serve() did not stop")
		}
	}
}

type shadowReporter struct {
	mu     sync.Mutex
	errors []error
}

func (reporter *shadowReporter) ReportIPCRejection(err error) {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	reporter.errors = append(reporter.errors, err)
}

func (reporter *shadowReporter) seen(target error, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		reporter.mu.Lock()
		for _, err := range reporter.errors {
			if errors.Is(err, target) {
				reporter.mu.Unlock()
				return true
			}
		}
		reporter.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func (reporter *shadowReporter) reports() []error {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	return append([]error(nil), reporter.errors...)
}
