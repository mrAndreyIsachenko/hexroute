package configpublish

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/configversion"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/objectstore"
)

const (
	envStoreEndpoint = "HEXROUTE_CONFIG_STORE_ENDPOINT"
	envStoreRegion   = "HEXROUTE_CONFIG_STORE_REGION"
	envStoreBucket   = "HEXROUTE_CONFIG_STORE_BUCKET"
	envStoreKeyID    = "HEXROUTE_CONFIG_STORE_ACCESS_KEY_ID"
	envStoreSecret   = "HEXROUTE_CONFIG_STORE_SECRET_KEY"
	envDatabaseURL   = "HEXROUTE_CONFIG_PUBLISHER_DATABASE_URL"
	envPublicKey     = "HEXROUTE_CONFIG_OPERATOR_PUBLIC_KEY"

	outputSchema = "hexroute.config-publish.v1"
	runTimeout   = 60 * time.Second
)

// ErrConfig is an incomplete or unusable environment.
var ErrConfig = errors.New("invalid configuration publisher environment")

type LookupEnv func(string) (string, bool)

type output struct {
	Schema        string `json:"schema"`
	ObjectKey     string `json:"object_key"`
	VersionLabel  string `json:"version_label"`
	ContentSHA256 string `json:"content_sha256"`
	SigningKeyID  string `json:"signing_key_id"`
}

// Run publishes one signed version.
//
// It reads a public key and never a private one. There is no path from here to
// a signature: a version this tool did not receive already signed is a version
// it refuses.
func Run(
	ctx context.Context,
	args []string,
	lookup LookupEnv,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if ctx == nil || lookup == nil || stdout == nil || stderr == nil {
		return 1
	}
	flags := flag.NewFlagSet("hexroute-config-publish", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	versionPath := flags.String("version", "", "signed configuration version file")
	check := flags.Bool("check", false, "validate the environment and exit")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		writeError(stderr, "usage")
		return 2
	}

	settings, err := loadSettings(lookup)
	if err != nil {
		writeError(stderr, "environment")
		return 1
	}
	if *check {
		return 0
	}
	if *versionPath == "" {
		writeError(stderr, "usage")
		return 2
	}
	encoded, err := readVersionFile(*versionPath)
	if err != nil {
		writeError(stderr, "unreadable_version")
		return 1
	}

	storage, err := objectstore.NewVersionStore(settings.store, objectstore.AccessPublish)
	if err != nil {
		writeError(stderr, "store")
		return 1
	}
	database, err := openDatabase(ctx, settings.databaseURL)
	if err != nil {
		writeError(stderr, "database")
		return 1
	}
	defer database.Close()
	ledger, err := NewPostgresLedger(database)
	if err != nil {
		writeError(stderr, "ledger")
		return 1
	}
	publisher, err := New(storage, ledger, settings.pinned)
	if err != nil {
		writeError(stderr, "publisher")
		return 1
	}
	versionID, err := metadata.NewUUID(nil)
	if err != nil {
		writeError(stderr, "version_id")
		return 1
	}
	publishCtx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	record, err := publisher.Publish(publishCtx, encoded, versionID, time.Now().UTC())
	if err != nil {
		writeError(stderr, publishFailure(err))
		return 1
	}
	_ = json.NewEncoder(stdout).Encode(output{
		Schema:        outputSchema,
		ObjectKey:     record.ObjectKey,
		VersionLabel:  record.VersionLabel,
		ContentSHA256: hexDigest(record),
		SigningKeyID:  record.SigningKeyID,
	})
	return 0
}

type settings struct {
	store       objectstore.Config
	databaseURL string
	pinned      ed25519.PublicKey
}

func loadSettings(lookup LookupEnv) (settings, error) {
	endpoint, endpointOK := requiredEnv(lookup, envStoreEndpoint)
	region, regionOK := requiredEnv(lookup, envStoreRegion)
	bucket, bucketOK := requiredEnv(lookup, envStoreBucket)
	keyID, keyIDOK := requiredEnv(lookup, envStoreKeyID)
	secret, secretOK := requiredEnv(lookup, envStoreSecret)
	databaseURL, databaseOK := requiredEnv(lookup, envDatabaseURL)
	encodedKey, publicKeyOK := requiredEnv(lookup, envPublicKey)
	if !endpointOK || !regionOK || !bucketOK || !keyIDOK || !secretOK ||
		!databaseOK || !publicKeyOK {
		return settings{}, ErrConfig
	}
	pinned, err := decodePublicKey(encodedKey)
	if err != nil {
		return settings{}, err
	}
	return settings{
		store: objectstore.Config{
			Endpoint:    endpoint,
			Region:      region,
			Bucket:      bucket,
			AccessKeyID: keyID,
			SecretKey:   secret,
		},
		databaseURL: databaseURL,
		pinned:      pinned,
	}, nil
}

func openDatabase(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, ErrConfig
	}
	config.MaxConns = 2
	config.MinConns = 0
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	database, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, ErrConfig
	}
	return database, nil
}

func decodePublicKey(encoded string) (ed25519.PublicKey, error) {
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding} {
		decoded, err := encoding.DecodeString(strings.TrimSpace(encoded))
		if err == nil && len(decoded) == ed25519.PublicKeySize {
			return ed25519.PublicKey(decoded), nil
		}
	}
	return nil, ErrConfig
}

func readVersionFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) && !strings.HasPrefix(path, ".") {
		path = "./" + path
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrConfig
	}
	content, err := io.ReadAll(io.LimitReader(file, configversion.MaxArtifactBytes+1))
	if err != nil || len(content) == 0 || len(content) > configversion.MaxArtifactBytes {
		return nil, ErrConfig
	}
	return content, nil
}

// publishFailure names the outcome without repeating anything private. An
// unverified version and a store that was unreachable call for different
// actions, and nothing else about either is the operator's business here.
func publishFailure(err error) string {
	switch {
	case errors.Is(err, ErrUnverified):
		return "unverified_version"
	case errors.Is(err, ErrLabelReused):
		return "label_reused"
	default:
		return "failed"
	}
}

func requiredEnv(lookup LookupEnv, name string) (string, bool) {
	value, ok := lookup(name)
	if !ok || value == "" || value != strings.TrimSpace(value) ||
		strings.ContainsAny(value, "\x00\r\n") {
		return "", false
	}
	return value, true
}

func hexDigest(record Record) string {
	return fmt.Sprintf("%x", record.ContentSHA256[:])
}

func writeError(stderr io.Writer, code string) {
	_, _ = fmt.Fprintf(stderr, "{\"schema\":%q,\"error\":%q}\n", outputSchema, code)
}
