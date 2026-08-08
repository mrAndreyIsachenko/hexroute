package policystore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"golang.org/x/sys/unix"
)

const (
	RootStorePath         = "/Library/Application Support/Hexroute/policy-root"
	UserStoreRelativePath = "Library/Application Support/Hexroute/policy-user"
	DirectoryMode         = os.FileMode(0o700)
	GenerationFileMode    = os.FileMode(0o400)
	MaxArtifactSize       = policy.MaxBundleArtifactSize

	generationsDirectory = "generations"
	stateDirectory       = "state"
)

type ArtifactKind string

const (
	ArtifactManifest ArtifactKind = "manifest"
	ArtifactPayload  ArtifactKind = "payload"
	ArtifactReview   ArtifactKind = "review"
	ArtifactApproval ArtifactKind = "approval"
)

type Generation struct {
	Bundle uint64
	Policy uint64
}

type Store struct {
	mu               sync.Mutex
	path             string
	domain           policy.Domain
	expectedUID      uint32
	expectedGID      uint32
	rootFD           int
	generationsFD    int
	stateFD          int
	rootDevice       uint64
	rootInode        uint64
	generationDev    uint64
	generationIno    uint64
	stateDevice      uint64
	stateInode       uint64
	persistenceFault persistenceFault
}

var (
	ErrInvalidStore       = errors.New("invalid policy store")
	ErrInsecureStore      = errors.New("policy store ownership or mode is invalid")
	ErrStoreUnavailable   = errors.New("policy store is unavailable")
	ErrStoreClosed        = errors.New("policy store is closed")
	ErrInvalidGeneration  = errors.New("invalid policy generation")
	ErrInvalidArtifact    = errors.New("invalid policy generation artifact")
	ErrInsecureArtifact   = errors.New("policy generation artifact ownership or mode is invalid")
	ErrGenerationExists   = errors.New("policy generation artifact already exists")
	ErrGenerationNotFound = errors.New("policy generation artifact not found")
)

func CurrentUserStorePath() (string, error) {
	home, _, _, err := currentUserIdentity()
	if err != nil {
		return "", err
	}
	return userStorePath(home)
}

func OpenRoot() (*Store, error) {
	return openStoreAt(RootStorePath, policy.DomainRoot, 0, 0)
}

func OpenCurrentUser() (*Store, error) {
	home, uid, gid, err := currentUserIdentity()
	if err != nil {
		return nil, err
	}
	path, err := userStorePath(home)
	if err != nil {
		return nil, err
	}
	return openStoreAt(path, policy.DomainUser, uid, gid)
}

func InitializeRoot() (*Store, error) {
	return initializeStoreAt(RootStorePath, policy.DomainRoot, 0, 0)
}

func InitializeCurrentUser() (*Store, error) {
	home, uid, gid, err := currentUserIdentity()
	if err != nil {
		return nil, err
	}
	path, err := userStorePath(home)
	if err != nil {
		return nil, err
	}
	return initializeStoreAt(path, policy.DomainUser, uid, gid)
}

func (store *Store) Domain() policy.Domain {
	if store == nil {
		return ""
	}
	return store.domain
}

func (store *Store) Close() error {
	if store == nil {
		return nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.rootFD < 0 && store.generationsFD < 0 && store.stateFD < 0 {
		return nil
	}
	failed := false
	if store.stateFD >= 0 {
		failed = unix.Close(store.stateFD) != nil
		store.stateFD = -1
	}
	if store.generationsFD >= 0 {
		failed = unix.Close(store.generationsFD) != nil || failed
		store.generationsFD = -1
	}
	if store.rootFD >= 0 {
		failed = unix.Close(store.rootFD) != nil || failed
		store.rootFD = -1
	}
	if failed {
		return ErrStoreUnavailable
	}
	return nil
}

func (store *Store) InstallArtifact(generation Generation, kind ArtifactKind, content []byte) error {
	name, err := generationFilename(storeDomain(store), generation, kind)
	if err != nil {
		return err
	}
	if len(content) == 0 || len(content) > MaxArtifactSize {
		return ErrInvalidArtifact
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return err
	}

	fd, err := unix.Openat(
		store.generationsFD,
		name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0o600,
	)
	if errors.Is(err, unix.EEXIST) {
		return store.classifyExistingLocked(name)
	}
	if err != nil {
		return ErrStoreUnavailable
	}
	success := false
	defer func() {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		if !success {
			_ = unix.Unlinkat(store.generationsFD, name, 0)
		}
	}()

	if err := writeAll(fd, content); err != nil ||
		unix.Fchmod(fd, uint32(GenerationFileMode.Perm())) != nil ||
		validateArtifactFD(fd, store.expectedUID, store.expectedGID) != nil ||
		unix.Fsync(fd) != nil {
		return ErrStoreUnavailable
	}
	if err := unix.Close(fd); err != nil {
		fd = -1
		return ErrStoreUnavailable
	}
	fd = -1
	if err := unix.Fsync(store.generationsFD); err != nil {
		return ErrStoreUnavailable
	}
	if err := store.validatePathBindingLocked(); err != nil {
		return err
	}
	success = true
	return nil
}

func (store *Store) ReadArtifact(generation Generation, kind ArtifactKind) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return nil, err
	}
	return store.readArtifactLocked(generation, kind)
}

func (store *Store) readArtifactLocked(generation Generation, kind ArtifactKind) ([]byte, error) {
	name, err := generationFilename(store.domain, generation, kind)
	if err != nil {
		return nil, err
	}

	fd, err := unix.Openat(
		store.generationsFD,
		name,
		unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if errors.Is(err, unix.ENOENT) {
		return nil, ErrGenerationNotFound
	}
	if err != nil {
		return nil, ErrInsecureArtifact
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrStoreUnavailable
	}
	defer file.Close()

	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil ||
		validateArtifactStat(stat, store.expectedUID, store.expectedGID) != nil {
		return nil, ErrInsecureArtifact
	}
	content, err := io.ReadAll(io.LimitReader(file, MaxArtifactSize+1))
	if err != nil || len(content) == 0 || len(content) > MaxArtifactSize || int64(len(content)) != stat.Size {
		return nil, ErrInvalidArtifact
	}
	if err := store.validatePathBindingLocked(); err != nil {
		return nil, err
	}
	return content, nil
}

func storeDomain(store *Store) policy.Domain {
	if store == nil {
		return ""
	}
	return store.domain
}

func userStorePath(home string) (string, error) {
	if !validAbsolutePath(home) || home == string(filepath.Separator) {
		return "", ErrInvalidStore
	}
	path := filepath.Join(home, UserStoreRelativePath)
	if !validAbsolutePath(path) {
		return "", ErrInvalidStore
	}
	return path, nil
}

func currentUserIdentity() (string, uint32, uint32, error) {
	account, err := user.Current()
	if err != nil || account.HomeDir == "" {
		return "", 0, 0, ErrInvalidStore
	}
	uidValue, uidErr := strconv.ParseUint(account.Uid, 10, 32)
	gidValue, gidErr := strconv.ParseUint(account.Gid, 10, 32)
	uid := uint32(uidValue)
	gid := uint32(gidValue)
	if uidErr != nil || gidErr != nil || uid != uint32(os.Geteuid()) || gid != uint32(os.Getegid()) {
		return "", 0, 0, ErrInvalidStore
	}
	return account.HomeDir, uid, gid, nil
}

func initializeStoreAt(path string, domain policy.Domain, uid, gid uint32) (*Store, error) {
	if !validStoreConfig(path, domain) || uint32(os.Geteuid()) != uid || uint32(os.Getegid()) != gid {
		return nil, ErrInvalidStore
	}
	parentFD, err := openDirectoryNoSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer unix.Close(parentFD)

	rootFD, rootCreated, err := ensureDirectoryAt(parentFD, filepath.Base(path), uid, gid)
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootFD)
	if rootCreated && unix.Fsync(parentFD) != nil {
		return nil, ErrStoreUnavailable
	}
	generationsFD, generationsCreated, err := ensureDirectoryAt(rootFD, generationsDirectory, uid, gid)
	if err != nil {
		return nil, err
	}
	if generationsCreated && unix.Fsync(rootFD) != nil {
		_ = unix.Close(generationsFD)
		return nil, ErrStoreUnavailable
	}
	if err := unix.Close(generationsFD); err != nil {
		return nil, ErrStoreUnavailable
	}
	stateFD, stateCreated, err := ensureDirectoryAt(rootFD, stateDirectory, uid, gid)
	if err != nil {
		return nil, err
	}
	if stateCreated && unix.Fsync(rootFD) != nil {
		_ = unix.Close(stateFD)
		return nil, ErrStoreUnavailable
	}
	if err := unix.Close(stateFD); err != nil {
		return nil, ErrStoreUnavailable
	}
	return openStoreAt(path, domain, uid, gid)
}

func openStoreAt(path string, domain policy.Domain, uid, gid uint32) (*Store, error) {
	if !validStoreConfig(path, domain) || uint32(os.Geteuid()) != uid || uint32(os.Getegid()) != gid {
		return nil, ErrInvalidStore
	}
	rootFD, err := openDirectoryNoSymlinks(path)
	if err != nil {
		return nil, err
	}
	if validateDirectoryFD(rootFD, uid, gid) != nil {
		_ = unix.Close(rootFD)
		return nil, ErrInsecureStore
	}
	generationsFD, err := unix.Openat(
		rootFD,
		generationsDirectory,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil || validateDirectoryFD(generationsFD, uid, gid) != nil {
		if generationsFD >= 0 {
			_ = unix.Close(generationsFD)
		}
		_ = unix.Close(rootFD)
		return nil, ErrInsecureStore
	}
	stateFD, err := unix.Openat(
		rootFD,
		stateDirectory,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil || validateDirectoryFD(stateFD, uid, gid) != nil {
		if stateFD >= 0 {
			_ = unix.Close(stateFD)
		}
		_ = unix.Close(generationsFD)
		_ = unix.Close(rootFD)
		return nil, ErrInsecureStore
	}
	rootDevice, rootInode, err := directoryIdentity(rootFD)
	if err != nil {
		_ = unix.Close(stateFD)
		_ = unix.Close(generationsFD)
		_ = unix.Close(rootFD)
		return nil, err
	}
	generationDev, generationIno, err := directoryIdentity(generationsFD)
	if err != nil {
		_ = unix.Close(stateFD)
		_ = unix.Close(generationsFD)
		_ = unix.Close(rootFD)
		return nil, err
	}
	stateDevice, stateInode, err := directoryIdentity(stateFD)
	if err != nil {
		_ = unix.Close(stateFD)
		_ = unix.Close(generationsFD)
		_ = unix.Close(rootFD)
		return nil, err
	}
	return &Store{
		path: path, domain: domain, expectedUID: uid, expectedGID: gid,
		rootFD: rootFD, generationsFD: generationsFD, stateFD: stateFD,
		rootDevice: rootDevice, rootInode: rootInode,
		generationDev: generationDev, generationIno: generationIno,
		stateDevice: stateDevice, stateInode: stateInode,
	}, nil
}

func (store *Store) validateOpenLocked() error {
	if store == nil || store.rootFD < 0 || store.generationsFD < 0 || store.stateFD < 0 {
		return ErrStoreClosed
	}
	if uint32(os.Geteuid()) != store.expectedUID || uint32(os.Getegid()) != store.expectedGID ||
		validateDirectoryFD(store.rootFD, store.expectedUID, store.expectedGID) != nil ||
		validateDirectoryFD(store.generationsFD, store.expectedUID, store.expectedGID) != nil ||
		validateDirectoryFD(store.stateFD, store.expectedUID, store.expectedGID) != nil {
		return ErrInsecureStore
	}
	return store.validatePathBindingLocked()
}

func (store *Store) validatePathBindingLocked() error {
	rootFD, err := openDirectoryNoSymlinks(store.path)
	if err != nil {
		return ErrInsecureStore
	}
	defer unix.Close(rootFD)
	if validateDirectoryFD(rootFD, store.expectedUID, store.expectedGID) != nil ||
		!sameDirectory(rootFD, store.rootDevice, store.rootInode) {
		return ErrInsecureStore
	}
	generationsFD, err := unix.Openat(
		rootFD,
		generationsDirectory,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return ErrInsecureStore
	}
	defer unix.Close(generationsFD)
	if validateDirectoryFD(generationsFD, store.expectedUID, store.expectedGID) != nil ||
		!sameDirectory(generationsFD, store.generationDev, store.generationIno) {
		return ErrInsecureStore
	}
	stateFD, err := unix.Openat(
		rootFD,
		stateDirectory,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return ErrInsecureStore
	}
	defer unix.Close(stateFD)
	if validateDirectoryFD(stateFD, store.expectedUID, store.expectedGID) != nil ||
		!sameDirectory(stateFD, store.stateDevice, store.stateInode) {
		return ErrInsecureStore
	}
	return nil
}

func (store *Store) classifyExistingLocked(name string) error {
	fd, err := unix.Openat(
		store.generationsFD,
		name,
		unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return ErrInsecureArtifact
	}
	defer unix.Close(fd)
	if validateArtifactFD(fd, store.expectedUID, store.expectedGID) != nil {
		return ErrInsecureArtifact
	}
	return ErrGenerationExists
}

func ensureDirectoryAt(parentFD int, name string, uid, gid uint32) (int, bool, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, filepath.Separator) {
		return -1, false, ErrInvalidStore
	}
	err := unix.Mkdirat(parentFD, name, uint32(DirectoryMode.Perm()))
	created := err == nil
	if err != nil && !errors.Is(err, unix.EEXIST) {
		return -1, false, ErrStoreUnavailable
	}
	fd, err := unix.Openat(
		parentFD,
		name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return -1, false, ErrInsecureStore
	}
	if created {
		// macOS inherits a new directory's group from its parent. Pin both
		// identities before validation so a root:admin parent can safely host
		// the required root:wheel policy store.
		if unix.Fchown(fd, int(uid), int(gid)) != nil ||
			unix.Fchmod(fd, uint32(DirectoryMode.Perm())) != nil {
			_ = unix.Close(fd)
			return -1, false, ErrStoreUnavailable
		}
	}
	if validateDirectoryFD(fd, uid, gid) != nil {
		_ = unix.Close(fd)
		return -1, false, ErrInsecureStore
	}
	return fd, created, nil
}

func openDirectoryNoSymlinks(path string) (int, error) {
	if !validAbsolutePath(path) || path == string(filepath.Separator) {
		return -1, ErrInvalidStore
	}
	fd, err := unix.Open(
		string(filepath.Separator),
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return -1, ErrStoreUnavailable
	}
	components := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			_ = unix.Close(fd)
			return -1, ErrInvalidStore
		}
		nextFD, openErr := unix.Openat(
			fd,
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
			0,
		)
		_ = unix.Close(fd)
		if openErr != nil {
			if errors.Is(openErr, unix.ENOENT) {
				return -1, ErrStoreUnavailable
			}
			return -1, ErrInsecureStore
		}
		fd = nextFD
	}
	return fd, nil
}

func validateDirectoryFD(fd int, uid, gid uint32) error {
	if fd < 0 {
		return ErrInsecureStore
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		os.FileMode(stat.Mode).Perm() != DirectoryMode || stat.Uid != uid || stat.Gid != gid {
		return ErrInsecureStore
	}
	return nil
}

func directoryIdentity(fd int) (uint64, uint64, error) {
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil {
		return 0, 0, ErrInsecureStore
	}
	return uint64(stat.Dev), uint64(stat.Ino), nil
}

func sameDirectory(fd int, device, inode uint64) bool {
	currentDevice, currentInode, err := directoryIdentity(fd)
	return err == nil && currentDevice == device && currentInode == inode
}

func validateArtifactFD(fd int, uid, gid uint32) error {
	if fd < 0 {
		return ErrInsecureArtifact
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil {
		return ErrInsecureArtifact
	}
	return validateArtifactStat(stat, uid, gid)
}

func validateArtifactStat(stat unix.Stat_t, uid, gid uint32) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		os.FileMode(stat.Mode).Perm() != GenerationFileMode ||
		stat.Uid != uid || stat.Gid != gid || stat.Nlink != 1 ||
		stat.Size <= 0 || stat.Size > MaxArtifactSize {
		return ErrInsecureArtifact
	}
	return nil
}

func generationFilename(domain policy.Domain, generation Generation, kind ArtifactKind) (string, error) {
	if !domain.Valid() || generation.Bundle == 0 || generation.Policy == 0 {
		return "", ErrInvalidGeneration
	}
	switch kind {
	case ArtifactManifest, ArtifactPayload, ArtifactReview, ArtifactApproval:
	default:
		return "", ErrInvalidArtifact
	}
	return fmt.Sprintf(
		"bundle-%020d-%s-%020d-%s.json",
		generation.Bundle,
		domain,
		generation.Policy,
		kind,
	), nil
}

func validStoreConfig(path string, domain policy.Domain) bool {
	return validAbsolutePath(path) && path != string(filepath.Separator) && domain.Valid()
}

func validAbsolutePath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func writeAll(fd int, content []byte) error {
	for len(content) > 0 {
		written, err := unix.Write(fd, content)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || written <= 0 {
			return ErrStoreUnavailable
		}
		content = content[written:]
	}
	return nil
}
