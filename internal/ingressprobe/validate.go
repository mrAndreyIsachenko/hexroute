package ingressprobe

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	minTimeout        = 100 * time.Millisecond
	maxTimeout        = 30 * time.Second
	maxHeartbeatAge   = 10 * time.Minute
	maxReferenceBytes = 128
)

var (
	hostnamePattern  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	referencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
)

func decodeStrict(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > RequestMaxBytes || target == nil {
		return errors.New("invalid request")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid request")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid request")
	}
	return nil
}

func timeoutDuration(milliseconds uint32) (time.Duration, bool) {
	duration := time.Duration(milliseconds) * time.Millisecond
	return duration, duration >= minTimeout && duration <= maxTimeout
}

func heartbeatAgeDuration(milliseconds uint32) (time.Duration, bool) {
	duration := time.Duration(milliseconds) * time.Millisecond
	return duration, duration >= minTimeout && duration <= maxHeartbeatAge
}

func validEndpoint(endpoint Endpoint) bool {
	return endpoint.Port != 0 && validHost(endpoint.Host)
}

func validHost(value string) bool {
	if value == "" || len(value) > 253 || strings.TrimSpace(value) != value {
		return false
	}
	if parsed := net.ParseIP(value); parsed != nil {
		return !parsed.IsUnspecified()
	}
	return hostnamePattern.MatchString(value) && !strings.Contains(value, "..")
}

func validServerName(value string) bool {
	return len(value) <= 253 && net.ParseIP(value) == nil && validHost(value)
}

func validReference(value string) bool {
	return len(value) <= maxReferenceBytes && referencePattern.MatchString(value)
}

func validHTTPSURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return parsed.Scheme == "https" && validHost(parsed.Hostname())
}

func validHeartbeatTransport(endpointURL, proxyURL string) bool {
	if proxyURL == "" {
		return validHTTPSURL(endpointURL)
	}
	if !validLoopbackSOCKSURL(proxyURL) {
		return false
	}
	return validHTTPSURL(endpointURL) || validLoopbackHTTPURL(endpointURL)
}

func validLoopbackSOCKSURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "socks5" && parsed.User == nil &&
		parsed.RawQuery == "" && parsed.Fragment == "" && parsed.Path == "" &&
		validLiteralLoopbackHostPort(parsed)
}

func validLoopbackHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "http" && parsed.User == nil &&
		parsed.RawQuery == "" && parsed.Fragment == "" &&
		validLiteralLoopbackHostPort(parsed)
}

func validLiteralLoopbackHostPort(parsed *url.URL) bool {
	if parsed == nil || parsed.Port() == "" {
		return false
	}
	port, err := strconv.ParseUint(parsed.Port(), 10, 16)
	if err != nil || port == 0 {
		return false
	}
	address := net.ParseIP(parsed.Hostname())
	return address != nil && address.IsLoopback()
}

func validUserID(value string) bool {
	_, err := metadata.ParseUUID(value)
	return err == nil
}

func validRealityPublicKey(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func validRealityShortID(value string) bool {
	if len(value) < 2 || len(value) > 16 || len(value)%2 != 0 ||
		strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) >= 1 && len(decoded) <= 8
}

func validOptionalSHA256(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func endpointAddress(endpoint Endpoint) string {
	return net.JoinHostPort(endpoint.Host, formatPort(endpoint.Port))
}

func formatPort(port uint16) string {
	const digits = "0123456789"
	if port == 0 {
		return "0"
	}
	var buffer [5]byte
	position := len(buffer)
	for port > 0 {
		position--
		buffer[position] = digits[port%10]
		port /= 10
	}
	return string(buffer[position:])
}
