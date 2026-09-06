package ingressagent

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/configversion"
	"github.com/mrAndreyIsachenko/hexroute/internal/objectstore"
)

const (
	envStoreEndpoint  = "HEXROUTE_AGENT_STORE_ENDPOINT"
	envStoreRegion    = "HEXROUTE_AGENT_STORE_REGION"
	envStoreBucket    = "HEXROUTE_AGENT_STORE_BUCKET"
	envStoreKeyID     = "HEXROUTE_AGENT_STORE_ACCESS_KEY_ID"
	envStoreSecret    = "HEXROUTE_AGENT_STORE_SECRET_KEY"
	envPublicKeyFile  = "HEXROUTE_AGENT_PUBLIC_KEY_FILE"
	envTargetKind     = "HEXROUTE_AGENT_TARGET_KIND"
	envTargetKey      = "HEXROUTE_AGENT_TARGET_KEY"
	envStateDir       = "HEXROUTE_AGENT_STATE_DIR"
	envConfigPath     = "HEXROUTE_AGENT_CONFIG_PATH"
	envGenerationPath = "HEXROUTE_AGENT_GENERATION_PATH"
	envReloadCommand  = "HEXROUTE_AGENT_RELOAD_COMMAND"
	envReloadArgs     = "HEXROUTE_AGENT_RELOAD_ARGS"
	envHeartbeatURL   = "HEXROUTE_AGENT_HEARTBEAT_URL"
	envIdentityFile   = "HEXROUTE_AGENT_NODE_IDENTITY_FILE"
	envProveWindow    = "HEXROUTE_AGENT_PROVE_WINDOW_SECONDS"

	// minProveWindow and maxProveWindow bound how long a version may go
	// unproven. Too short and a host merely slow to come back is returned
	// from; too long and a broken version serves for hours with nothing
	// having decided that it should.
	minProveWindow = time.Minute
	maxProveWindow = 24 * time.Hour

	maxPublicKeyFileBytes = 1024
)

var reloadArgument = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@:/-]{0,127}$`)

// LookupEnv reads the process environment.
type LookupEnv func(string) (string, bool)

// Config is everything one agent needs. Every field is required: an agent that
// defaulted its bucket, its target or its public key would be an agent nobody
// decided the behaviour of.
type Config struct {
	Store          objectstore.Config
	PublicKeyFile  string
	Target         configversion.Target
	StateDir       string
	ConfigPath     string
	GenerationPath string
	ReloadCommand  string
	ReloadArgs     []string
	HeartbeatURL   string
	IdentityFile   string
	ProveWindow    time.Duration
}

// LoadConfig reads the agent's configuration from the environment.
func LoadConfig(lookup LookupEnv) (Config, error) {
	if lookup == nil {
		return Config{}, fmt.Errorf("%w: no environment", ErrAgent)
	}
	endpoint, endpointOK := requiredEnv(lookup, envStoreEndpoint)
	region, regionOK := requiredEnv(lookup, envStoreRegion)
	bucket, bucketOK := requiredEnv(lookup, envStoreBucket)
	keyID, keyIDOK := requiredEnv(lookup, envStoreKeyID)
	secret, secretOK := requiredEnv(lookup, envStoreSecret)
	publicKeyFile, publicKeyOK := requiredEnv(lookup, envPublicKeyFile)
	targetKind, kindOK := requiredEnv(lookup, envTargetKind)
	targetKey, keyOK := requiredEnv(lookup, envTargetKey)
	stateDir, stateOK := requiredEnv(lookup, envStateDir)
	configPath, configOK := requiredEnv(lookup, envConfigPath)
	generationPath, generationOK := requiredEnv(lookup, envGenerationPath)
	reloadCommand, reloadOK := requiredEnv(lookup, envReloadCommand)
	heartbeatURL, heartbeatOK := requiredEnv(lookup, envHeartbeatURL)
	identityFile, identityOK := requiredEnv(lookup, envIdentityFile)
	rawWindow, windowOK := requiredEnv(lookup, envProveWindow)
	if !endpointOK || !regionOK || !bucketOK || !keyIDOK || !secretOK ||
		!publicKeyOK || !kindOK || !keyOK || !stateOK || !configOK ||
		!generationOK || !reloadOK || !heartbeatOK || !identityOK || !windowOK {
		return Config{}, fmt.Errorf("%w: incomplete environment", ErrAgent)
	}
	if !validAbsolutePath(publicKeyFile) || !validAbsolutePath(stateDir) ||
		!validAbsolutePath(configPath) || !validAbsolutePath(generationPath) ||
		!validAbsolutePath(reloadCommand) || !validAbsolutePath(identityFile) ||
		configPath == generationPath {
		return Config{}, fmt.Errorf("%w: paths must be absolute", ErrAgent)
	}
	seconds, err := strconv.Atoi(rawWindow)
	if err != nil {
		return Config{}, fmt.Errorf("%w: prove window", ErrAgent)
	}
	window := time.Duration(seconds) * time.Second
	if window < minProveWindow || window > maxProveWindow {
		return Config{}, fmt.Errorf("%w: prove window", ErrAgent)
	}
	target := configversion.Target{
		Kind: configversion.TargetKind(targetKind),
		Key:  targetKey,
	}
	switch target.Kind {
	case configversion.TargetNode, configversion.TargetGroup, configversion.TargetGlobal:
	default:
		return Config{}, fmt.Errorf("%w: target kind", ErrAgent)
	}
	args, err := reloadArguments(lookup)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Store: objectstore.Config{
			Endpoint:    endpoint,
			Region:      region,
			Bucket:      bucket,
			AccessKeyID: keyID,
			SecretKey:   secret,
		},
		PublicKeyFile:  publicKeyFile,
		Target:         target,
		StateDir:       stateDir,
		ConfigPath:     configPath,
		GenerationPath: generationPath,
		ReloadCommand:  reloadCommand,
		ReloadArgs:     args,
		HeartbeatURL:   heartbeatURL,
		IdentityFile:   identityFile,
		ProveWindow:    window,
	}, nil
}

// ReadPinnedKey reads the operator public key placed on this host when it was
// built.
//
// There is no default and no fallback. An agent that could not find its key
// applies nothing, which is the only safe reading of "I do not know who is
// allowed to change what I run".
func ReadPinnedKey(path string) (ed25519.PublicKey, error) {
	if !validAbsolutePath(path) {
		return nil, fmt.Errorf("%w: public key path", ErrAgent)
	}
	content, err := os.ReadFile(path)
	if err != nil || len(content) == 0 || len(content) > maxPublicKeyFileBytes {
		return nil, fmt.Errorf("%w: public key file", ErrAgent)
	}
	encoded := strings.TrimSpace(string(content))
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding} {
		decoded, err := encoding.DecodeString(encoded)
		if err == nil && len(decoded) == ed25519.PublicKeySize {
			return ed25519.PublicKey(decoded), nil
		}
	}
	return nil, fmt.Errorf("%w: public key encoding", ErrAgent)
}

func reloadArguments(lookup LookupEnv) ([]string, error) {
	raw, ok := lookup(envReloadArgs)
	if !ok || raw == "" {
		return nil, nil
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(fields) > 8 {
		return nil, fmt.Errorf("%w: reload arguments", ErrAgent)
	}
	for _, field := range fields {
		if !reloadArgument.MatchString(field) {
			return nil, fmt.Errorf("%w: reload argument", ErrAgent)
		}
	}
	return fields, nil
}

func requiredEnv(lookup LookupEnv, name string) (string, bool) {
	value, ok := lookup(name)
	if !ok || value == "" || value != strings.TrimSpace(value) ||
		strings.ContainsAny(value, "\x00\r\n") {
		return "", false
	}
	return value, true
}

func validAbsolutePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path &&
		!strings.ContainsAny(path, "\x00\r\n") && net.ParseIP(path) == nil
}
