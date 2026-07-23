package credentials

import (
	"bytes"
	"context"
	"errors"
	"regexp"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

const (
	securityCommand = "/usr/bin/security"
	maxSecretBytes  = 4096
)

var keychainIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,127}$`)

type KeychainConfig struct {
	Account     string
	PINService  string
	TOTPService string
}

type KeychainSource struct {
	runner observe.Runner
	config KeychainConfig
}

func NewKeychainSource(
	runner observe.Runner,
	config KeychainConfig,
) (*KeychainSource, error) {
	if runner == nil || !config.valid() {
		return nil, errors.New("invalid Keychain configuration")
	}
	return &KeychainSource{
		runner: runner,
		config: config,
	}, nil
}

func (source *KeychainSource) ReadPritunl(ctx context.Context) (*Pritunl, error) {
	totpSeed, err := source.read(ctx, source.config.TOTPService)
	if err != nil {
		return nil, err
	}
	defer clear(totpSeed)

	pin, err := source.read(ctx, source.config.PINService)
	if err != nil {
		return nil, err
	}
	defer clear(pin)

	return newPritunl(pin, totpSeed)
}

func (source *KeychainSource) read(ctx context.Context, service string) ([]byte, error) {
	output, err := source.runner.Output(
		ctx,
		securityCommand,
		"find-generic-password",
		"-s",
		service,
		"-a",
		source.config.Account,
		"-w",
	)
	if err != nil {
		return nil, err
	}
	defer clear(output)

	output = bytes.TrimSuffix(output, []byte("\n"))
	output = bytes.TrimSuffix(output, []byte("\r"))
	if len(output) == 0 || len(output) > maxSecretBytes {
		return nil, errors.New("invalid Keychain secret")
	}
	return append([]byte(nil), output...), nil
}

func (config KeychainConfig) valid() bool {
	return keychainIdentifier.MatchString(config.Account) &&
		keychainIdentifier.MatchString(config.PINService) &&
		keychainIdentifier.MatchString(config.TOTPService) &&
		config.PINService != config.TOTPService
}

var _ Source = (*KeychainSource)(nil)
