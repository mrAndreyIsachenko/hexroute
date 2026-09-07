package policystore

import (
	"crypto/ed25519"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
)

// afterStartupExpiry is past the fixture's own validity window. Every test here
// evaluates at that moment, because the whole question is what remains
// derivable once a generation is over.
var afterStartupExpiry = time.Date(2030, time.January, 2, 0, 0, 0, 0, time.UTC)

func TestLineageSurvivesExpiryThatStopsActiveRevalidation(t *testing.T) {
	for _, domain := range []policy.Domain{policy.DomainRoot, policy.DomainUser} {
		t.Run(string(domain), func(t *testing.T) {
			store, _ := newTestStore(t, domain)
			fixture := newStartupFixture(t, domain, 1)
			installStartupFixture(t, store, fixture)

			if _, err := store.RecoverActive(
				fixture.installed, fixture.publicKey, afterStartupExpiry,
			); !errors.Is(err, policyapproval.ErrApprovalExpired) {
				t.Fatalf("RecoverActive() past expiry = %v, want ErrApprovalExpired", err)
			}

			lineage, err := store.RecoverLineage(
				fixture.installed, fixture.publicKey, afterStartupExpiry,
			)
			if err != nil {
				t.Fatalf("RecoverLineage() past expiry: %v", err)
			}
			if lineage.Generation != fixture.generation ||
				lineage.PayloadSHA256 != fixture.payloadDigest ||
				lineage.Domain != domain {
				t.Fatalf("RecoverLineage() = %+v, want the installed generation", lineage)
			}
			if !lineage.Expired {
				t.Fatal("RecoverLineage() reported a lapsed generation as current")
			}
			if lineage.StaticSuperseded {
				t.Fatal("RecoverLineage() reported a matching static digest as superseded")
			}
		})
	}
}

func TestLineageSurvivesASupersededSafetyEnvelope(t *testing.T) {
	store, _ := newTestStore(t, policy.DomainUser)
	fixture := newStartupFixture(t, policy.DomainUser, 1)
	installStartupFixture(t, store, fixture)

	// The envelope gained a capability, so the installed static digest no longer
	// matches the one the active generation was compiled against.
	installed := fixture.installed
	installed.StaticSHA256 = policy.SHA256Hex([]byte("envelope-after-a-new-capability"))

	if _, err := store.RecoverActive(
		installed, fixture.publicKey, insideStartupWindow(),
	); !errors.Is(err, policy.ErrRestartRequired) {
		t.Fatalf("RecoverActive() under a new envelope = %v, want ErrRestartRequired", err)
	}

	lineage, err := store.RecoverLineage(installed, fixture.publicKey, afterStartupExpiry)
	if err != nil {
		t.Fatalf("RecoverLineage() under a new envelope: %v", err)
	}
	if lineage.Generation != fixture.generation {
		t.Fatalf("RecoverLineage() = %+v, want the installed generation", lineage)
	}
	if !lineage.StaticSuperseded {
		t.Fatal("RecoverLineage() did not report the predecessor's static digest as superseded")
	}
	if lineage.StaticSHA256 != fixture.manifest.StaticSHA256 {
		t.Fatal("RecoverLineage() reported the installed static digest instead of the predecessor's")
	}
}

func TestLineageRefusesDamagedEvidence(t *testing.T) {
	wrongKey := make(ed25519.PublicKey, ed25519.PublicKeySize)

	cases := map[string]struct {
		mutate func(*testing.T, string, *startupFixture) (policy.InstalledCompatibility, ed25519.PublicKey)
	}{
		"signed by another key": {
			mutate: func(_ *testing.T, _ string, fixture *startupFixture) (policy.InstalledCompatibility, ed25519.PublicKey) {
				return fixture.installed, wrongKey
			},
		},
		"produced by an untrusted compiler": {
			mutate: func(_ *testing.T, _ string, fixture *startupFixture) (policy.InstalledCompatibility, ed25519.PublicKey) {
				installed := fixture.installed
				installed.TrustedCompilerSHA256 = []string{policy.SHA256Hex([]byte("some other compiler"))}
				return installed, fixture.publicKey
			},
		},
		"payload replaced on disk": {
			mutate: func(t *testing.T, path string, fixture *startupFixture) (policy.InstalledCompatibility, ed25519.PublicKey) {
				replaceGenerationArtifact(t, path, fixture.generation, policy.DomainUser, ArtifactPayload, []byte(`{"schema":"x"}`))
				return fixture.installed, fixture.publicKey
			},
		},
		"approval replaced on disk": {
			mutate: func(t *testing.T, path string, fixture *startupFixture) (policy.InstalledCompatibility, ed25519.PublicKey) {
				replaceGenerationArtifact(t, path, fixture.generation, policy.DomainUser, ArtifactApproval, []byte(`{"schema":"x"}`))
				return fixture.installed, fixture.publicKey
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			store, path := newTestStore(t, policy.DomainUser)
			fixture := newStartupFixture(t, policy.DomainUser, 1)
			installStartupFixture(t, store, fixture)
			installed, key := testCase.mutate(t, path, fixture)

			if _, err := store.RecoverLineage(installed, key, afterStartupExpiry); err == nil {
				t.Fatal("RecoverLineage() accepted damaged evidence; the chain is what is proven")
			}
		})
	}
}

// TestLineageCarriesNothingThatCouldAuthorize is a compile-time statement as
// much as a test: if the record ever gains a manifest, payload or approval, a
// caller can evaluate policy against a generation that no longer governs, and
// the mistake looks like an ordinary field access.
func TestLineageCarriesNothingThatCouldAuthorize(t *testing.T) {
	store, _ := newTestStore(t, policy.DomainUser)
	fixture := newStartupFixture(t, policy.DomainUser, 1)
	installStartupFixture(t, store, fixture)

	lineage, err := store.RecoverLineage(fixture.installed, fixture.publicKey, afterStartupExpiry)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]struct{}{
		"Manifest": {}, "Payload": {}, "Approval": {}, "Review": {}, "Intent": {}, "Rules": {},
	}
	for _, field := range structFieldNames(lineage) {
		if _, banned := forbidden[field]; banned {
			t.Fatalf("Lineage carries %q, which is enough to authorize from a lapsed generation", field)
		}
	}
}

func insideStartupWindow() time.Time {
	return time.Date(2030, time.January, 1, 0, 30, 0, 0, time.UTC)
}

func structFieldNames(value any) []string {
	typ := reflect.TypeOf(value)
	names := make([]string, 0, typ.NumField())
	for index := 0; index < typ.NumField(); index++ {
		names = append(names, typ.Field(index).Name)
	}
	return names
}
