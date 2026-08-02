package policystore

import (
	"bytes"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"golang.org/x/sys/unix"
)

func TestFixedStorePathsAreDisjointAndDerived(t *testing.T) {
	if RootStorePath != "/Library/Application Support/Hexroute/policy-root" {
		t.Fatalf("unexpected root store path: %q", RootStorePath)
	}
	userPath, err := userStorePath("/Users/synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if userPath != "/Users/synthetic/Library/Application Support/Hexroute/policy-user" ||
		userPath == RootStorePath {
		t.Fatalf("unexpected user store path: %q", userPath)
	}
	for _, invalid := range []string{"", "relative", "/Users/synthetic/..", "/"} {
		if _, err := userStorePath(invalid); !errors.Is(err, ErrInvalidStore) {
			t.Fatalf("userStorePath(%q) error = %v", invalid, err)
		}
	}
}

func TestCurrentUserStorePathIgnoresHOMEEnvironment(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "/tmp/hexroute-untrusted-home")
	path, err := CurrentUserStorePath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(account.HomeDir, UserStoreRelativePath)
	if path != want {
		t.Fatalf("current user store path = %q, want %q", path, want)
	}
}

func TestGenerationFilenameIsTypedAndDomainBound(t *testing.T) {
	generation := Generation{Bundle: 7, Policy: 3}
	rootName, err := generationFilename(policy.DomainRoot, generation, ArtifactPayload)
	if err != nil {
		t.Fatal(err)
	}
	userName, err := generationFilename(policy.DomainUser, generation, ArtifactPayload)
	if err != nil {
		t.Fatal(err)
	}
	if rootName != "bundle-00000000000000000007-root-00000000000000000003-payload.json" ||
		userName != "bundle-00000000000000000007-user-00000000000000000003-payload.json" ||
		rootName == userName {
		t.Fatalf("unexpected generation names: root=%q user=%q", rootName, userName)
	}
	reviewName, err := generationFilename(policy.DomainRoot, generation, ArtifactReview)
	if err != nil || reviewName != "bundle-00000000000000000007-root-00000000000000000003-review.json" {
		t.Fatalf("unexpected review filename: name=%q err=%v", reviewName, err)
	}
	for _, test := range []struct {
		generation Generation
		kind       ArtifactKind
	}{
		{Generation{}, ArtifactPayload},
		{Generation{Bundle: 1}, ArtifactPayload},
		{Generation{Bundle: 1, Policy: 1}, ArtifactKind("../payload")},
	} {
		if _, err := generationFilename(policy.DomainRoot, test.generation, test.kind); err == nil {
			t.Fatalf("invalid generation filename accepted: %+v %q", test.generation, test.kind)
		}
	}
}

func TestStoreInstallsAndReadsImmutableGenerationArtifacts(t *testing.T) {
	store, path := newTestStore(t, policy.DomainRoot)
	defer store.Close()
	generation := Generation{Bundle: 11, Policy: 4}
	content := []byte(`{"schema":"synthetic.policy.v1"}`)

	if err := store.InstallArtifact(generation, ArtifactPayload, content); err != nil {
		t.Fatalf("install artifact: %v", err)
	}
	read, err := store.ReadArtifact(generation, ArtifactPayload)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !bytes.Equal(read, content) {
		t.Fatalf("read content = %q", read)
	}
	if err := store.InstallArtifact(generation, ArtifactPayload, []byte("replacement")); !errors.Is(err, ErrGenerationExists) {
		t.Fatalf("replacement error = %v", err)
	}
	read, err = store.ReadArtifact(generation, ArtifactPayload)
	if err != nil || !bytes.Equal(read, content) {
		t.Fatalf("immutable content changed: content=%q err=%v", read, err)
	}

	name, _ := generationFilename(policy.DomainRoot, generation, ArtifactPayload)
	artifactPath := filepath.Join(path, generationsDirectory, name)
	info, err := os.Lstat(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != GenerationFileMode || stat.Uid != uint32(os.Geteuid()) ||
		stat.Gid != uint32(os.Getegid()) || stat.Nlink != 1 {
		t.Fatalf("artifact metadata is not immutable and private: mode=%v stat=%+v", info.Mode(), stat)
	}
}

func TestStoreRejectsSymlinkedOrInsecureDirectories(t *testing.T) {
	t.Run("store symlink", func(t *testing.T) {
		store, path := newTestStore(t, policy.DomainUser)
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		realPath := path + "-real"
		if err := os.Rename(path, realPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(realPath, path); err != nil {
			t.Fatal(err)
		}
		if _, err := openStoreAt(path, policy.DomainUser, currentUID(), currentGID()); !errors.Is(err, ErrInsecureStore) {
			t.Fatalf("symlinked store error = %v", err)
		}
	})

	t.Run("parent symlink", func(t *testing.T) {
		base := realTempDir(t)
		realParent := filepath.Join(base, "real")
		if err := os.Mkdir(realParent, 0o700); err != nil {
			t.Fatal(err)
		}
		store, err := initializeStoreAt(
			filepath.Join(realParent, "store"), policy.DomainUser, currentUID(), currentGID(),
		)
		if err != nil {
			t.Fatal(err)
		}
		_ = store.Close()
		linkedParent := filepath.Join(base, "linked")
		if err := os.Symlink(realParent, linkedParent); err != nil {
			t.Fatal(err)
		}
		if _, err := openStoreAt(
			filepath.Join(linkedParent, "store"), policy.DomainUser, currentUID(), currentGID(),
		); !errors.Is(err, ErrInsecureStore) {
			t.Fatalf("symlinked parent error = %v", err)
		}
	})

	t.Run("wrong mode", func(t *testing.T) {
		store, path := newTestStore(t, policy.DomainUser)
		_ = store.Close()
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := openStoreAt(path, policy.DomainUser, currentUID(), currentGID()); !errors.Is(err, ErrInsecureStore) {
			t.Fatalf("insecure mode error = %v", err)
		}
	})

	t.Run("wrong owner expectation", func(t *testing.T) {
		store, path := newTestStore(t, policy.DomainUser)
		_ = store.Close()
		fd, err := openDirectoryNoSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fd)
		if err := validateDirectoryFD(fd, currentUID()+1, currentGID()); !errors.Is(err, ErrInsecureStore) {
			t.Fatalf("wrong owner expectation error = %v", err)
		}
	})
}

func TestOpenStoreRejectsFixedPathReplacement(t *testing.T) {
	store, path := newTestStore(t, policy.DomainRoot)
	defer store.Close()
	movedPath := path + "-moved"
	if err := os.Rename(path, movedPath); err != nil {
		t.Fatal(err)
	}
	replacement, err := initializeStoreAt(path, policy.DomainRoot, currentUID(), currentGID())
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if err := store.InstallArtifact(
		Generation{Bundle: 1, Policy: 1}, ArtifactPayload, []byte("payload"),
	); !errors.Is(err, ErrInsecureStore) {
		t.Fatalf("replaced fixed path error = %v", err)
	}
}

func TestStoreRejectsSymlinkNonRegularModeAndHardLinkArtifacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, artifactPath string)
	}{
		{
			name: "symlink",
			mutate: func(t *testing.T, artifactPath string) {
				target := artifactPath + ".target"
				if err := os.WriteFile(target, []byte("target"), GenerationFileMode); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(artifactPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, artifactPath); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "non-regular",
			mutate: func(t *testing.T, artifactPath string) {
				if err := os.Remove(artifactPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(artifactPath, GenerationFileMode); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "writable mode",
			mutate: func(t *testing.T, artifactPath string) {
				if err := os.Chmod(artifactPath, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "hard link",
			mutate: func(t *testing.T, artifactPath string) {
				if err := os.Link(artifactPath, artifactPath+".link"); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, path := newTestStore(t, policy.DomainRoot)
			defer store.Close()
			generation := Generation{Bundle: 2, Policy: 1}
			if err := store.InstallArtifact(generation, ArtifactManifest, []byte("manifest")); err != nil {
				t.Fatal(err)
			}
			name, _ := generationFilename(policy.DomainRoot, generation, ArtifactManifest)
			artifactPath := filepath.Join(path, generationsDirectory, name)
			test.mutate(t, artifactPath)
			if _, err := store.ReadArtifact(generation, ArtifactManifest); !errors.Is(err, ErrInsecureArtifact) {
				t.Fatalf("insecure artifact error = %v", err)
			}
			if err := store.InstallArtifact(generation, ArtifactManifest, []byte("replacement")); !errors.Is(err, ErrInsecureArtifact) {
				t.Fatalf("insecure existing artifact error = %v", err)
			}
		})
	}
}

func TestStoreRejectsMissingInvalidAndClosedOperations(t *testing.T) {
	store, _ := newTestStore(t, policy.DomainUser)
	generation := Generation{Bundle: 1, Policy: 1}
	if _, err := store.ReadArtifact(generation, ArtifactApproval); !errors.Is(err, ErrGenerationNotFound) {
		t.Fatalf("missing artifact error = %v", err)
	}
	if err := store.InstallArtifact(Generation{}, ArtifactApproval, []byte("approval")); !errors.Is(err, ErrInvalidGeneration) {
		t.Fatalf("invalid generation error = %v", err)
	}
	if err := store.InstallArtifact(generation, ArtifactKind("../approval"), []byte("approval")); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("invalid artifact kind error = %v", err)
	}
	if err := store.InstallArtifact(generation, ArtifactApproval, nil); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("empty artifact error = %v", err)
	}
	if err := store.InstallArtifact(
		generation, ArtifactApproval, bytes.Repeat([]byte("x"), MaxArtifactSize+1),
	); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("oversized artifact error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadArtifact(generation, ArtifactApproval); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("closed store error = %v", err)
	}
}

func newTestStore(t *testing.T, domain policy.Domain) (*Store, string) {
	t.Helper()
	path := filepath.Join(realTempDir(t), "store")
	store, err := initializeStoreAt(path, domain, currentUID(), currentGID())
	if err != nil {
		t.Fatalf("initialize store: %v", err)
	}
	if store.Domain() != domain {
		t.Fatalf("store domain = %q, want %q", store.Domain(), domain)
	}
	return store, path
}

func realTempDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func currentUID() uint32 { return uint32(os.Geteuid()) }
func currentGID() uint32 { return uint32(os.Getegid()) }
