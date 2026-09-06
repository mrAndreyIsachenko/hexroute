package configprove

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/ingressprobe"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	envDatabaseURL    = "HEXROUTE_PROVE_DATABASE_URL"
	envTargetKind     = "HEXROUTE_PROVE_TARGET_KIND"
	envTargetKey      = "HEXROUTE_PROVE_TARGET_KEY"
	envHeartbeatURL   = "HEXROUTE_PROVE_HEARTBEAT_URL"
	envProxyURL       = "HEXROUTE_PROVE_PROXY_URL"
	envNodeID         = "HEXROUTE_PROVE_NODE_ID"
	envKeyID          = "HEXROUTE_PROVE_KEY_ID"
	envPublicKey      = "HEXROUTE_PROVE_NODE_PUBLIC_KEY"
	envWindowSeconds  = "HEXROUTE_PROVE_WINDOW_SECONDS"
	envDeadlineSecond = "HEXROUTE_PROVE_DEADLINE_SECONDS"

	outputSchema      = "hexroute.config-prove.v1"
	runTimeout        = 60 * time.Second
	heartbeatTimeout  = 15 * time.Second
	heartbeatMaxAgeMS = 120000
)

// LookupEnv reads the process environment.
type LookupEnv func(string) (string, bool)

// Run reads one host's heartbeat and records what it establishes.
//
// The instrument is the ingress probe, unchanged: it already pulls the signed
// heartbeat through the tunnel and checks that the generation is the expected
// one. This adds the connection to the ledger and nothing else.
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
	flags := flag.NewFlagSet("hexroute-config-prove", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	check := flags.Bool("check", false, "validate the environment and exit")
	version := flags.Bool("version", false, "print the build and exit")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		writeError(stderr, "usage")
		return 2
	}
	if *version {
		_, _ = fmt.Fprintf(stdout, "hexroute-config-prove version=%s commit=%s\n",
			buildinfo.Version, buildinfo.Commit)
		return 0
	}

	settings, err := loadSettings(lookup)
	if err != nil {
		writeError(stderr, "environment")
		return 1
	}
	if *check {
		return 0
	}

	runContext, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	database, err := openDatabase(runContext, settings.databaseURL)
	if err != nil {
		writeError(stderr, "database")
		return 1
	}
	defer database.Close()
	ledger, err := NewPostgresLedger(database, buildinfo.Version)
	if err != nil {
		writeError(stderr, "ledger")
		return 1
	}
	prover, err := New(ledger, settings.window, settings.deadline)
	if err != nil {
		writeError(stderr, "prover")
		return 1
	}

	current, err := ledger.LatestVersion(runContext, settings.targetKind, settings.targetKey)
	if err != nil {
		writeError(stderr, "no_version")
		return 1
	}
	observation := observe(runContext, settings, current.VersionLabel)
	deploymentID, err := metadata.NewUUID(nil)
	if err != nil {
		writeError(stderr, "deployment_id")
		return 1
	}
	result, err := prover.Prove(runContext, settings.targetKind, settings.targetKey,
		observation, deploymentID, time.Now().UTC())
	if err != nil {
		writeError(stderr, "failed")
		return 1
	}
	_ = json.NewEncoder(stdout).Encode(struct {
		Schema string `json:"schema"`
		Result
	}{Schema: outputSchema, Result: result})
	return 0
}

// observe asks the probe whether that exact generation is serving.
//
// The probe's own categories are the answer: it distinguishes a host running
// something else from a host running this and failing, which is the difference
// between "not deployed" and "deployed and broken".
func observe(ctx context.Context, settings settings, label string) Observation {
	request, err := json.Marshal(ingressprobe.HeartbeatRequest{
		EndpointURL:        settings.heartbeatURL,
		ProxyURL:           settings.proxyURL,
		ExpectedNodeID:     settings.nodeID,
		ExpectedKeyID:      settings.keyID,
		PublicKey:          settings.publicKey,
		ExpectedGeneration: label,
		TimeoutMS:          uint32(heartbeatTimeout / time.Millisecond),
		MaxAgeMS:           heartbeatMaxAgeMS,
	})
	if err != nil {
		return Observation{}
	}
	result := ingressprobe.DefaultRunner().Probe(ctx, ingressprobe.KindHeartbeat, request)
	switch result.Category {
	case ingressprobe.CategoryOK:
		return Observation{Generation: label, Healthy: true}
	case ingressprobe.CategoryHeartbeatUnhealthy:
		return Observation{Generation: label}
	case ingressprobe.CategoryHeartbeatGeneration:
		// The host answered, and it is running something else.
		return Observation{Generation: "unreported"}
	default:
		return Observation{}
	}
}

type settings struct {
	databaseURL string
	targetKind  string
	targetKey   string
	heartbeatURL,
	proxyURL,
	nodeID,
	keyID,
	publicKey string
	window   time.Duration
	deadline time.Duration
}

func loadSettings(lookup LookupEnv) (settings, error) {
	values := map[string]string{}
	for _, name := range []string{
		envDatabaseURL, envTargetKind, envTargetKey, envHeartbeatURL,
		envNodeID, envKeyID, envPublicKey, envWindowSeconds, envDeadlineSecond,
	} {
		value, ok := requiredEnv(lookup, name)
		if !ok {
			return settings{}, fmt.Errorf("%w: %s", ErrProve, "incomplete environment")
		}
		values[name] = value
	}
	window, err := seconds(values[envWindowSeconds])
	if err != nil {
		return settings{}, err
	}
	deadline, err := seconds(values[envDeadlineSecond])
	if err != nil {
		return settings{}, err
	}
	proxyURL, _ := lookup(envProxyURL)
	return settings{
		databaseURL:  values[envDatabaseURL],
		targetKind:   values[envTargetKind],
		targetKey:    values[envTargetKey],
		heartbeatURL: values[envHeartbeatURL],
		proxyURL:     strings.TrimSpace(proxyURL),
		nodeID:       values[envNodeID],
		keyID:        values[envKeyID],
		publicKey:    values[envPublicKey],
		window:       window,
		deadline:     deadline,
	}, nil
}

func seconds(raw string) (time.Duration, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > 7*24*3600 {
		return 0, fmt.Errorf("%w: duration", ErrProve)
	}
	return time.Duration(value) * time.Second, nil
}

func openDatabase(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: database url", ErrProve)
	}
	config.MaxConns = 2
	config.MinConns = 0
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("%w: database", ErrProve)
	}
	return pool, nil
}

func requiredEnv(lookup LookupEnv, name string) (string, bool) {
	value, ok := lookup(name)
	if !ok || value == "" || value != strings.TrimSpace(value) ||
		strings.ContainsAny(value, "\x00\r\n") {
		return "", false
	}
	return value, true
}

func writeError(stderr io.Writer, code string) {
	_, _ = fmt.Fprintf(stderr, "{\"schema\":%q,\"error\":%q}\n", outputSchema, code)
}
