package policyinstaller

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyconfig"
	"github.com/mrAndreyIsachenko/hexroute/internal/policystore"
	"golang.org/x/sys/unix"
)

const (
	resultSchema   = "hexroute.policy-install.v1"
	rootConfigPath = "/Library/Application Support/Hexroute/observe-root/config/root-observe.json"
	userConfigRel  = "Library/Application Support/Hexroute/observe-user/config/user-observe.json"
	rootConfig     = "hexroute.root-observe.v1"
	userConfig     = "hexroute.user-observe.v1"
)

var (
	errInvalidArguments = errors.New("invalid installer arguments")
	errUnauthorized     = errors.New("installer privilege boundary rejected")
	errInvalidBundle    = errors.New("invalid signed policy bundle")
	errInvalidConfig    = errors.New("invalid installed policy configuration")
	errInstallFailed    = errors.New("policy artifact installation failed")
)

type result struct {
	Schema           string        `json:"schema"`
	Command          string        `json:"command"`
	Domain           policy.Domain `json:"domain"`
	BundleGeneration uint64        `json:"bundle_generation,omitempty"`
	PolicyGeneration uint64        `json:"policy_generation,omitempty"`
	ManifestSHA256   string        `json:"manifest_sha256,omitempty"`
}

type observerConfig struct {
	Schema        string                     `json:"schema"`
	PolicyControl *policyconfig.StaticConfig `json:"policy_control"`
}

type signedBundle struct {
	Candidate policy.CandidateBundle
	Review    policyapproval.ReviewReport
	Approval  policyapproval.SignedApproval
	Artifacts map[policystore.ArtifactKind][]byte
}

type artifactStore interface {
	Domain() policy.Domain
	InstallArtifact(policystore.Generation, policystore.ArtifactKind, []byte) error
	ReadArtifact(policystore.Generation, policystore.ArtifactKind) ([]byte, error)
	Close() error
}

type installStore interface {
	artifactStore
	RecoverActive(
		policy.InstalledCompatibility,
		ed25519.PublicKey,
		time.Time,
	) (policystore.RevalidatedActive, error)
}

func Run(args []string, stdout, stderr io.Writer) int {
	if stdout == nil || stderr == nil {
		return 1
	}
	if len(args) == 1 && args[0] == "--check" {
		return 0
	}
	if len(args) == 0 {
		writeError(stderr, "usage")
		return 2
	}
	var output result
	var err error
	switch args[0] {
	case "init":
		output, err = runInit(args[1:])
	case "install":
		output, err = runInstall(args[1:])
	default:
		writeError(stderr, "usage")
		return 2
	}
	if err != nil {
		writeError(stderr, failureCode(err))
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(output); err != nil {
		return 1
	}
	return 0
}

func runInit(args []string) (result, error) {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	domainValue := flags.String("domain", "", "root or user policy domain")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return result{}, errInvalidArguments
	}
	domain, err := parseDomain(*domainValue)
	if err != nil {
		return result{}, err
	}
	store, err := initializeStore(domain)
	if err != nil {
		return result{}, err
	}
	if err := store.Close(); err != nil {
		return result{}, errInstallFailed
	}
	return result{Schema: resultSchema, Command: "init", Domain: domain}, nil
}

func runInstall(args []string) (result, error) {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	domainValue := flags.String("domain", "", "root or user policy domain")
	candidatePath := flags.String("candidate", "", "private canonical candidate directory")
	signedPath := flags.String("signed", "", "private signed review directory")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *candidatePath == "" || *signedPath == "" {
		return result{}, errInvalidArguments
	}
	domain, err := parseDomain(*domainValue)
	if err != nil {
		return result{}, err
	}
	ownerUID, err := sourceOwnerUID(domain)
	if err != nil {
		return result{}, err
	}
	bundle, err := readSignedBundle(*candidatePath, *signedPath, ownerUID, domain)
	if err != nil {
		return result{}, err
	}
	runtime, err := loadInstalledPolicyConfig(domain)
	if err != nil {
		return result{}, err
	}
	store, err := initializeStore(domain)
	if err != nil {
		return result{}, err
	}
	defer store.Close()
	now := time.Now().UTC()
	runtime, err = runtimeForInstall(store, runtime, now)
	if err != nil {
		return result{}, err
	}
	if err := installCandidate(store, runtime, bundle, now); err != nil {
		return result{}, err
	}
	manifest := bundle.Candidate.Manifest
	generation := manifest.Root.Generation
	if domain == policy.DomainUser {
		generation = manifest.User.Generation
	}
	return result{
		Schema: resultSchema, Command: "install", Domain: domain,
		BundleGeneration: manifest.BundleGeneration,
		PolicyGeneration: generation,
		ManifestSHA256:   bundle.Candidate.ManifestSHA256,
	}, nil
}

func runtimeForInstall(
	store installStore,
	runtime policyconfig.RuntimeConfig,
	now time.Time,
) (policyconfig.RuntimeConfig, error) {
	if store == nil || runtime.Validate() != nil || store.Domain() != runtime.Installed.Domain || now.IsZero() {
		return policyconfig.RuntimeConfig{}, errInvalidConfig
	}
	active, err := store.RecoverActive(runtime.Installed, runtime.PinnedPublicKey, now)
	if errors.Is(err, policystore.ErrRecordNotFound) {
		if runtime.Installed.CurrentBundleGeneration != 0 ||
			runtime.Installed.CurrentPolicyGeneration != 0 ||
			runtime.Installed.CurrentPayloadSHA256 != "" {
			return policyconfig.RuntimeConfig{}, errInvalidConfig
		}
		return runtime, nil
	}
	if err != nil || active.ConfirmedAt == "" || active.Domain != store.Domain() ||
		active.Generation.Bundle == 0 || active.Generation.Policy == 0 ||
		active.Manifest.BundleGeneration != active.Generation.Bundle ||
		active.Payload.PolicyGeneration != active.Generation.Policy ||
		active.PayloadSHA256 == "" {
		return policyconfig.RuntimeConfig{}, errInvalidConfig
	}
	runtime.Installed.CurrentPolicySchema = active.Manifest.PolicySchema
	runtime.Installed.CurrentBundleGeneration = active.Generation.Bundle
	runtime.Installed.CurrentPolicyGeneration = active.Generation.Policy
	runtime.Installed.CurrentPayloadSHA256 = active.PayloadSHA256
	if runtime.Validate() != nil {
		return policyconfig.RuntimeConfig{}, errInvalidConfig
	}
	return runtime, nil
}

func installCandidate(
	store artifactStore,
	runtime policyconfig.RuntimeConfig,
	bundle signedBundle,
	now time.Time,
) error {
	if store == nil || !store.Domain().Valid() || runtime.Validate() != nil ||
		store.Domain() != runtime.Installed.Domain || bundle.Candidate.Validate() != nil || now.IsZero() {
		return errInstallFailed
	}
	domain := store.Domain()
	payload := bundle.Candidate.Root
	if domain == policy.DomainUser {
		payload = bundle.Candidate.User
	}
	if err := policyapproval.VerifyDomainCandidate(
		bundle.Candidate.Manifest,
		bundle.Candidate.ManifestSHA256,
		payload,
		bundle.Review,
		bundle.Approval,
		ed25519.PublicKey(runtime.PinnedPublicKey),
		now,
	); err != nil {
		return errInvalidBundle
	}
	if err := policy.CheckCandidateCompatibility(
		bundle.Candidate.Manifest,
		payload,
		runtime.Installed,
	); err != nil {
		return errInvalidConfig
	}
	generation := policystore.Generation{
		Bundle: bundle.Candidate.Manifest.BundleGeneration,
		Policy: payload.PolicyGeneration,
	}
	for _, kind := range []policystore.ArtifactKind{
		policystore.ArtifactManifest,
		policystore.ArtifactPayload,
		policystore.ArtifactReview,
		policystore.ArtifactApproval,
	} {
		content := bundle.Artifacts[kind]
		if len(content) == 0 {
			return errInvalidBundle
		}
		if err := installArtifact(store, generation, kind, content); err != nil {
			return err
		}
	}
	return nil
}

func installArtifact(
	store artifactStore,
	generation policystore.Generation,
	kind policystore.ArtifactKind,
	content []byte,
) error {
	err := store.InstallArtifact(generation, kind, content)
	if err == nil {
		return nil
	}
	if !errors.Is(err, policystore.ErrGenerationExists) {
		return errInstallFailed
	}
	existing, readErr := store.ReadArtifact(generation, kind)
	if readErr != nil || !bytes.Equal(existing, content) {
		return errInstallFailed
	}
	return nil
}

func readSignedBundle(candidatePath, signedPath string, ownerUID uint32, domain policy.Domain) (signedBundle, error) {
	candidateArtifacts, err := readPrivateDirectory(
		candidatePath,
		[]string{"manifest.json", "root.json", "user.json"},
		ownerUID,
	)
	if err != nil {
		return signedBundle{}, errInvalidBundle
	}
	signedArtifacts, err := readPrivateDirectory(
		signedPath,
		[]string{"approval.json", "review.json"},
		ownerUID,
	)
	if err != nil {
		return signedBundle{}, errInvalidBundle
	}
	candidate, err := policy.DecodeCandidateBundle(
		candidateArtifacts["manifest.json"],
		candidateArtifacts["root.json"],
		candidateArtifacts["user.json"],
	)
	if err != nil {
		return signedBundle{}, errInvalidBundle
	}
	review, err := policyapproval.DecodeReviewArtifact(signedArtifacts["review.json"])
	if err != nil {
		return signedBundle{}, errInvalidBundle
	}
	approval, err := policyapproval.DecodeApprovalArtifact(signedArtifacts["approval.json"])
	if err != nil {
		return signedBundle{}, errInvalidBundle
	}
	payload := candidateArtifacts["root.json"]
	if domain == policy.DomainUser {
		payload = candidateArtifacts["user.json"]
	}
	return signedBundle{
		Candidate: candidate,
		Review:    review,
		Approval:  approval,
		Artifacts: map[policystore.ArtifactKind][]byte{
			policystore.ArtifactManifest: candidateArtifacts["manifest.json"],
			policystore.ArtifactPayload:  payload,
			policystore.ArtifactReview:   signedArtifacts["review.json"],
			policystore.ArtifactApproval: signedArtifacts["approval.json"],
		},
	}, nil
}

func readPrivateDirectory(path string, expected []string, ownerUID uint32) (map[string][]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) ||
		len(expected) == 0 {
		return nil, errInvalidBundle
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errInvalidBundle
	}
	defer unix.Close(fd)
	var directoryStat unix.Stat_t
	if unix.Fstat(fd, &directoryStat) != nil || directoryStat.Uid != ownerUID ||
		os.FileMode(directoryStat.Mode).Perm() != 0o700 {
		return nil, errInvalidBundle
	}
	dupFD, err := unix.Dup(fd)
	if err != nil {
		return nil, errInvalidBundle
	}
	directory := os.NewFile(uintptr(dupFD), "policy-bundle")
	if directory == nil {
		unix.Close(dupFD)
		return nil, errInvalidBundle
	}
	names, err := directory.Readdirnames(-1)
	_ = directory.Close()
	if err != nil {
		return nil, errInvalidBundle
	}
	sort.Strings(names)
	expectedNames := append([]string(nil), expected...)
	sort.Strings(expectedNames)
	if len(names) != len(expectedNames) {
		return nil, errInvalidBundle
	}
	for index := range names {
		if names[index] != expectedNames[index] {
			return nil, errInvalidBundle
		}
	}
	artifacts := make(map[string][]byte, len(expectedNames))
	for _, name := range expectedNames {
		content, err := readPrivateFileAt(fd, name, ownerUID)
		if err != nil {
			return nil, err
		}
		artifacts[name] = content
	}
	return artifacts, nil
}

func readPrivateFileAt(directoryFD int, name string, ownerUID uint32) ([]byte, error) {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return nil, errInvalidBundle
	}
	fd, err := unix.Openat(
		directoryFD,
		name,
		unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return nil, errInvalidBundle
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		unix.Close(fd)
		return nil, errInvalidBundle
	}
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != ownerUID || stat.Nlink != 1 ||
		stat.Mode&unix.S_IFMT != unix.S_IFREG || os.FileMode(stat.Mode).Perm() != 0o600 ||
		stat.Size <= 0 || stat.Size > int64(policy.MaxCanonicalJSONSize) {
		return nil, errInvalidBundle
	}
	content, err := io.ReadAll(io.LimitReader(file, policy.MaxCanonicalJSONSize+1))
	if err != nil || int64(len(content)) != stat.Size {
		return nil, errInvalidBundle
	}
	return content, nil
}

func loadInstalledPolicyConfig(domain policy.Domain) (policyconfig.RuntimeConfig, error) {
	path, schema, ownerUID, err := installedConfigIdentity(domain)
	if err != nil {
		return policyconfig.RuntimeConfig{}, err
	}
	return readInstalledPolicyConfig(path, schema, ownerUID, domain)
}

func readInstalledPolicyConfig(
	path string,
	schema string,
	ownerUID uint32,
	domain policy.Domain,
) (policyconfig.RuntimeConfig, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return policyconfig.RuntimeConfig{}, errInvalidConfig
	}
	file := os.NewFile(uintptr(fd), "installed-policy-config")
	if file == nil {
		unix.Close(fd)
		return policyconfig.RuntimeConfig{}, errInvalidConfig
	}
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != ownerUID || stat.Nlink != 1 ||
		stat.Mode&unix.S_IFMT != unix.S_IFREG || os.FileMode(stat.Mode).Perm() != 0o600 ||
		stat.Size <= 0 || stat.Size > 64*1024 {
		return policyconfig.RuntimeConfig{}, errInvalidConfig
	}
	var config observerConfig
	decoder := json.NewDecoder(io.LimitReader(file, 64*1024+1))
	if decoder.Decode(&config) != nil || config.Schema != schema || config.PolicyControl == nil {
		return policyconfig.RuntimeConfig{}, errInvalidConfig
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return policyconfig.RuntimeConfig{}, errInvalidConfig
	}
	runtime, err := config.PolicyControl.Runtime(domain)
	if err != nil {
		return policyconfig.RuntimeConfig{}, errInvalidConfig
	}
	return runtime, nil
}

func installedConfigIdentity(domain policy.Domain) (string, string, uint32, error) {
	switch domain {
	case policy.DomainRoot:
		if os.Geteuid() != 0 {
			return "", "", 0, errUnauthorized
		}
		return rootConfigPath, rootConfig, 0, nil
	case policy.DomainUser:
		if os.Geteuid() == 0 {
			return "", "", 0, errUnauthorized
		}
		account, err := user.Current()
		if err != nil || account.HomeDir == "" {
			return "", "", 0, errUnauthorized
		}
		uid, err := strconv.ParseUint(account.Uid, 10, 32)
		if err != nil || uint32(uid) != uint32(os.Geteuid()) {
			return "", "", 0, errUnauthorized
		}
		return filepath.Join(account.HomeDir, userConfigRel), userConfig, uint32(uid), nil
	default:
		return "", "", 0, errInvalidArguments
	}
}

func initializeStore(domain policy.Domain) (installStore, error) {
	switch domain {
	case policy.DomainRoot:
		if os.Geteuid() != 0 {
			return nil, errUnauthorized
		}
		store, err := policystore.InitializeRoot()
		if err != nil {
			return nil, err
		}
		return store, nil
	case policy.DomainUser:
		if os.Geteuid() == 0 {
			return nil, errUnauthorized
		}
		store, err := policystore.InitializeCurrentUser()
		if err != nil {
			return nil, err
		}
		return store, nil
	default:
		return nil, errInvalidArguments
	}
}

func sourceOwnerUID(domain policy.Domain) (uint32, error) {
	if domain == policy.DomainUser {
		if os.Geteuid() == 0 {
			return 0, errUnauthorized
		}
		return uint32(os.Geteuid()), nil
	}
	if domain != policy.DomainRoot || os.Geteuid() != 0 {
		return 0, errUnauthorized
	}
	value := os.Getenv("SUDO_UID")
	if value == "" {
		return 0, nil
	}
	uid, err := strconv.ParseUint(value, 10, 32)
	if err != nil || uid == 0 {
		return 0, errUnauthorized
	}
	return uint32(uid), nil
}

func parseDomain(value string) (policy.Domain, error) {
	domain := policy.Domain(value)
	if !domain.Valid() {
		return "", errInvalidArguments
	}
	return domain, nil
}

func failureCode(err error) string {
	switch {
	case errors.Is(err, errInvalidArguments):
		return "invalid_arguments"
	case errors.Is(err, errUnauthorized):
		return "unauthorized"
	case errors.Is(err, errInvalidBundle):
		return "invalid_bundle"
	case errors.Is(err, errInvalidConfig):
		return "invalid_config"
	case errors.Is(err, policystore.ErrInvalidStore):
		return "invalid_store"
	case errors.Is(err, policystore.ErrInsecureStore):
		return "insecure_store"
	case errors.Is(err, policystore.ErrStoreUnavailable):
		return "store_unavailable"
	default:
		return "install_failed"
	}
}

func writeError(writer io.Writer, code string) {
	_, _ = fmt.Fprintf(writer, "hexroute-policy-installer: %s\n", code)
}
