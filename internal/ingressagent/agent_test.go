package ingressagent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/configversion"
)

type stubFetcher struct {
	objects map[string][]byte
	err     error
	calls   int
	keys    []string
}

func (fetcher *stubFetcher) GetVersion(_ context.Context, key string) ([]byte, error) {
	fetcher.calls++
	fetcher.keys = append(fetcher.keys, key)
	if fetcher.err != nil {
		return nil, fetcher.err
	}
	content, ok := fetcher.objects[key]
	if !ok {
		return nil, errors.New("no such object")
	}
	return content, nil
}

type stubApplier struct {
	applied     [][]byte
	generations []string
	err         error
	failOn      int
}

func (applier *stubApplier) Apply(_ context.Context, content []byte, generation string) error {
	applier.applied = append(applier.applied, append([]byte(nil), content...))
	applier.generations = append(applier.generations, generation)
	if applier.err != nil && (applier.failOn == 0 || applier.failOn == len(applier.applied)) {
		return applier.err
	}
	return nil
}

type localSigner struct{ private ed25519.PrivateKey }

func (signer localSigner) PublicKey() (ed25519.PublicKey, error) {
	return signer.private.Public().(ed25519.PublicKey), nil
}

func (signer localSigner) Sign(message []byte) ([]byte, error) {
	return ed25519.Sign(signer.private, message), nil
}

func agentTarget() configversion.Target {
	return configversion.Target{Kind: configversion.TargetNode, Key: "ingress-provider-b"}
}

func currentKey() string {
	return configversion.CurrentKey(string(agentTarget().Kind), agentTarget().Key)
}

func operatorKey(seed byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
}

func version(t *testing.T, private ed25519.PrivateKey, label string, at time.Time, content []byte) []byte {
	t.Helper()
	artifact, err := configversion.Publish(
		localSigner{private: private}, agentTarget(), label, content, at)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := configversion.Encode(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func newAgent(t *testing.T, fetcher *stubFetcher, applier *stubApplier, pinned ed25519.PublicKey) (*Agent, *Store) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := New(fetcher, applier, store, pinned, agentTarget())
	if err != nil {
		t.Fatal(err)
	}
	return agent, store
}

func TestTheAgentAppliesAVersionItVerified(t *testing.T) {
	private := operatorKey(7)
	content := []byte(`{"inbounds":[{"port":443}]}`)
	encoded := version(t, private, "2026-09-06.1",
		time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC), content)
	fetcher := &stubFetcher{objects: map[string][]byte{currentKey(): encoded}}
	applier := &stubApplier{}
	agent, store := newAgent(t, fetcher, applier, private.Public().(ed25519.PublicKey))

	result, err := agent.Sync(context.Background())
	if err != nil || result.Outcome != OutcomeApplied {
		t.Fatalf("sync: %+v %v", result, err)
	}
	if len(applier.applied) != 1 || !bytes.Equal(applier.applied[0], content) {
		t.Fatal("the applied bytes are not the ones signed")
	}
	// The host is told which version it is now running, so its heartbeat can
	// report it and the version can be proven by what is serving.
	if applier.generations[0] != "2026-09-06.1" {
		t.Fatalf("generation = %q", applier.generations[0])
	}

	// The host asks; nothing tells it. The only key it reads is the one it
	// derives from its own target.
	if len(fetcher.keys) != 1 || fetcher.keys[0] != currentKey() {
		t.Fatalf("keys read: %v", fetcher.keys)
	}

	// Running it again changes nothing, which is what makes a timer safe.
	second, err := agent.Sync(context.Background())
	if err != nil || second.Outcome != OutcomeUnchanged || len(applier.applied) != 1 {
		t.Fatalf("second sync: %+v %v", second, err)
	}
	if applied, _, ok, err := store.Applied(); err != nil || !ok ||
		applied.Statement.VersionLabel != "2026-09-06.1" {
		t.Fatalf("applied state: %v %v", ok, err)
	}
}

// Every one of these leaves the host serving what it already serves, and every
// one is recorded as the check it was.
func TestARefusedVersionLeavesTheHostAloneAndSaysWhichCheckFailed(t *testing.T) {
	private := operatorKey(7)
	other := operatorKey(9)
	running := []byte(`{"inbounds":[{"port":443}]}`)
	at := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)

	for _, testCase := range []struct {
		name   string
		object []byte
		reason string
	}{
		{
			name:   "signed by a key this host was not built to trust",
			object: version(t, other, "2026-09-07.1", at.Add(time.Hour), []byte(`{"inbounds":[]}`)),
			reason: "unpinned_key",
		},
		{
			name:   "bytes from the expected place that are not a version",
			object: []byte(`{"inbounds":[{"port":8080}]}`),
			reason: "malformed",
		},
		{
			name:   "content substituted after signing",
			object: substitute(t, version(t, private, "2026-09-07.1", at.Add(time.Hour), []byte(`{"a":1}`))),
			reason: "digest_mismatch",
		},
		{
			name:   "a version older than the one running",
			object: version(t, private, "2026-09-05.1", at.Add(-time.Hour), []byte(`{"inbounds":[]}`)),
			reason: "not_newer_than_running",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			first := version(t, private, "2026-09-06.1", at, running)
			fetcher := &stubFetcher{objects: map[string][]byte{currentKey(): first}}
			applier := &stubApplier{}
			agent, store := newAgent(t, fetcher, applier, private.Public().(ed25519.PublicKey))
			if _, err := agent.Sync(context.Background()); err != nil {
				t.Fatal(err)
			}

			fetcher.objects[currentKey()] = testCase.object
			result, err := agent.Sync(context.Background())
			if err != nil || result.Outcome != OutcomeRefused {
				t.Fatalf("sync: %+v %v", result, err)
			}
			if result.Reason != testCase.reason {
				t.Fatalf("reason = %q, want %q", result.Reason, testCase.reason)
			}
			// Still serving the first version, and nothing was applied twice.
			if len(applier.applied) != 1 || !bytes.Equal(applier.applied[0], running) {
				t.Fatalf("applications: %d", len(applier.applied))
			}
			applied, _, ok, err := store.Applied()
			if err != nil || !ok || applied.Statement.VersionLabel != "2026-09-06.1" {
				t.Fatal("the refused version replaced the running one")
			}
			recorded, ok, err := store.LastResult()
			if err != nil || !ok || recorded.Reason != testCase.reason ||
				recorded.Outcome != OutcomeRefused || recorded.At == "" {
				t.Fatalf("recorded: %+v", recorded)
			}
		})
	}
}

// The store being unreachable is not a refusal and not a failure: the host has
// a configuration and keeps running it.
func TestAnUnreachableStoreIsNotARefusal(t *testing.T) {
	private := operatorKey(7)
	fetcher := &stubFetcher{err: errors.New("connection refused")}
	applier := &stubApplier{}
	agent, store := newAgent(t, fetcher, applier, private.Public().(ed25519.PublicKey))

	result, err := agent.Sync(context.Background())
	if err != nil || result.Outcome != OutcomeUnreachable {
		t.Fatalf("sync: %+v %v", result, err)
	}
	if len(applier.applied) != 0 {
		t.Fatal("an unreachable store still changed the configuration")
	}
	recorded, ok, err := store.LastResult()
	if err != nil || !ok || recorded.Outcome != OutcomeUnreachable {
		t.Fatalf("recorded: %+v", recorded)
	}
}

// Returning is the reason the previous version is kept as bytes rather than as
// a name. A host that has lost the store is the host most likely to need it.
func TestReturningToTheRetainedVersionNeedsNoNetwork(t *testing.T) {
	private := operatorKey(7)
	first := []byte(`{"inbounds":[{"port":443}]}`)
	second := []byte(`{"inbounds":[{"port":8443}]}`)
	at := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	fetcher := &stubFetcher{objects: map[string][]byte{
		currentKey(): version(t, private, "2026-09-06.1", at, first),
	}}
	applier := &stubApplier{}
	agent, store := newAgent(t, fetcher, applier, private.Public().(ed25519.PublicKey))
	if _, err := agent.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	fetcher.objects[currentKey()] = version(t, private, "2026-09-07.1", at.Add(time.Hour), second)
	if _, err := agent.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	callsBefore := fetcher.calls
	result, err := agent.Return(context.Background())
	if err != nil || result.Outcome != OutcomeReturned ||
		result.VersionLabel != "2026-09-06.1" {
		t.Fatalf("return: %+v %v", result, err)
	}
	if fetcher.calls != callsBefore {
		t.Fatal("returning reached the network")
	}
	if len(applier.applied) != 3 || !bytes.Equal(applier.applied[2], first) {
		t.Fatal("the returned configuration is not the retained one")
	}
	applied, _, ok, err := store.Applied()
	if err != nil || !ok || applied.Statement.VersionLabel != "2026-09-06.1" {
		t.Fatal("the return did not become the applied version")
	}
	// A second return would go back into the version that was just left.
	if _, _, retained, err := store.Retained(); err != nil || retained {
		t.Fatal("a returned-from version is still retained")
	}
	if _, err := agent.Return(context.Background()); !errors.Is(err, ErrNoRetainedVersion) {
		t.Fatalf("second return: %v", err)
	}
}

// An apply that fails leaves the host with whatever the applier managed to do,
// so the retained configuration is put back rather than left to chance.
func TestAFailedApplyPutsTheRetainedConfigurationBack(t *testing.T) {
	private := operatorKey(7)
	first := []byte(`{"inbounds":[{"port":443}]}`)
	at := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	fetcher := &stubFetcher{objects: map[string][]byte{
		currentKey(): version(t, private, "2026-09-06.1", at, first),
	}}
	applier := &stubApplier{}
	agent, store := newAgent(t, fetcher, applier, private.Public().(ed25519.PublicKey))
	if _, err := agent.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	applier.err = errors.New("the runtime refused to reload")
	applier.failOn = 2
	fetcher.objects[currentKey()] = version(t, private, "2026-09-07.1",
		at.Add(time.Hour), []byte(`{"inbounds":[{"port":8443}]}`))
	result, err := agent.Sync(context.Background())
	if err != nil || result.Outcome != OutcomeRefused || result.Reason != "apply_failed" {
		t.Fatalf("sync: %+v %v", result, err)
	}
	if len(applier.applied) != 3 || !bytes.Equal(applier.applied[2], first) {
		t.Fatal("the retained configuration was not put back")
	}
	applied, _, ok, err := store.Applied()
	if err != nil || !ok || applied.Statement.VersionLabel != "2026-09-06.1" {
		t.Fatal("a failed apply was recorded as applied")
	}
}

func TestAnAgentWithoutAPinnedKeyIsRefused(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name   string
		pinned ed25519.PublicKey
		target configversion.Target
	}{
		{name: "no key", target: agentTarget()},
		{name: "a truncated key", pinned: make(ed25519.PublicKey, 16), target: agentTarget()},
		{name: "no target", pinned: operatorKey(7).Public().(ed25519.PublicKey)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := New(&stubFetcher{}, &stubApplier{}, store,
				testCase.pinned, testCase.target); !errors.Is(err, ErrAgent) {
				t.Fatalf("accepted %s: %v", testCase.name, err)
			}
		})
	}
}

func substitute(t *testing.T, encoded []byte) []byte {
	t.Helper()
	artifact, err := configversion.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Content = base64.RawURLEncoding.EncodeToString([]byte(`{"inbounds":[{"port":9}]}`))
	substituted, err := configversion.Encode(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return substituted
}
