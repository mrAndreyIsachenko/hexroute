package ingressobserver

import (
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	envListenAddr       = "HEXROUTE_OBSERVER_LISTEN_ADDR"
	envXRayEndpoint     = "HEXROUTE_OBSERVER_XRAY_ENDPOINT"
	envOutboundEndpoint = "HEXROUTE_OBSERVER_OUTBOUND_ENDPOINT"
	envNodeID           = "HEXROUTE_OBSERVER_NODE_ID"
	envGeneration       = "HEXROUTE_OBSERVER_GENERATION"
	envKeyFile          = "HEXROUTE_OBSERVER_KEY_FILE"
)

var (
	ErrInvalidConfig = errors.New("invalid ingress observer configuration")
	referencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

type LookupEnv func(string) (string, bool)

type Config struct {
	ListenAddr       string
	XRayEndpoint     string
	OutboundEndpoint string
	NodeID           metadata.UUID
	Generation       string
	KeyFile          string
}

func LoadConfig(lookup LookupEnv) (Config, error) {
	if lookup == nil {
		return Config{}, ErrInvalidConfig
	}
	listenAddr, listenOK := requiredEnv(lookup, envListenAddr)
	xrayEndpoint, xrayOK := requiredEnv(lookup, envXRayEndpoint)
	outboundEndpoint, outboundOK := requiredEnv(lookup, envOutboundEndpoint)
	rawNodeID, nodeOK := requiredEnv(lookup, envNodeID)
	generation, generationOK := requiredEnv(lookup, envGeneration)
	keyFile, keyOK := requiredEnv(lookup, envKeyFile)
	nodeID, nodeErr := metadata.ParseUUID(rawNodeID)
	if !listenOK || !xrayOK || !outboundOK || !nodeOK || !generationOK ||
		!keyOK || nodeErr != nil || !validLoopbackEndpoint(listenAddr) ||
		!validLoopbackEndpoint(xrayEndpoint) ||
		!validPublicLiteralEndpoint(outboundEndpoint) ||
		!referencePattern.MatchString(generation) || !validAbsolutePath(keyFile) {
		return Config{}, ErrInvalidConfig
	}
	return Config{
		ListenAddr:       listenAddr,
		XRayEndpoint:     xrayEndpoint,
		OutboundEndpoint: outboundEndpoint,
		NodeID:           nodeID,
		Generation:       generation,
		KeyFile:          keyFile,
	}, nil
}

func requiredEnv(lookup LookupEnv, name string) (string, bool) {
	value, ok := lookup(name)
	if !ok || value == "" || value != strings.TrimSpace(value) ||
		strings.ContainsAny(value, "\x00\r\n") {
		return "", false
	}
	return value, true
}

func validLoopbackEndpoint(raw string) bool {
	address, err := netip.ParseAddrPort(raw)
	return err == nil && address.Port() != 0 && address.Addr().Is4() &&
		address.Addr().IsLoopback() &&
		raw == address.String()
}

func validPublicLiteralEndpoint(raw string) bool {
	address, err := netip.ParseAddrPort(raw)
	if err != nil || address.Port() == 0 || raw != address.String() {
		return false
	}
	host := address.Addr().Unmap()
	return host.Is4() && host.IsGlobalUnicast() && !host.IsPrivate() &&
		!host.IsLoopback()
}

func validAbsolutePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path &&
		!strings.ContainsAny(path, "\x00\r\n") && net.ParseIP(path) == nil
}
