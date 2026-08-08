package repositoryguard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

var ErrUnsafeArtifact = errors.New("unsafe repository artifact")

const secretCanaryFixture = "testdata/secrets/v1/canaries.json"

var documentationPrefixes = []netip.Prefix{
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
}

var forbiddenPolicySchemas = map[string]struct{}{
	"hexroute.effective-policy.v1":       {},
	"hexroute.policy-manifest.v1":        {},
	"hexroute.policy-domain.v1":          {},
	"hexroute.policy-review.v1":          {},
	"hexroute.policy-approval.v1":        {},
	"hexroute.policy-prepare-receipt.v1": {},
	"hexroute.policy-commit-intent.v1":   {},
	"hexroute.policy-active-pointer.v1":  {},
	"hexroute.action-lease.v1":           {},
	"hexroute.action-lease-execution.v1": {},
	"hexroute.action-lease-outcome.v1":   {},
}

// ValidateArtifact enforces the public-repository boundary for policy-related
// structured files. It accepts examples only when their identifiers and
// network values are explicitly synthetic.
func ValidateArtifact(relativePath string, data []byte) error {
	path := filepath.ToSlash(filepath.Clean(relativePath))
	if path == "." || path == "" || strings.HasPrefix(path, "../") || unsafePath(path) {
		return unsafe(relativePath, "forbidden path")
	}

	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".json" && extension != ".yaml" && extension != ".yml" {
		return nil
	}
	if bytes.Contains(data, []byte("HEXROUTE_CANARY_")) && path != secretCanaryFixture {
		return unsafe(relativePath, "secret canary outside its fixture")
	}
	if extension == ".json" {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var document any
		if err := decoder.Decode(&document); err != nil {
			return unsafe(relativePath, "malformed structured file")
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return unsafe(relativePath, "malformed structured file")
		}
		return validateValue(path, document, false)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return unsafe(relativePath, "malformed structured file")
		}
		if err := validateNode(path, &document, false); err != nil {
			return err
		}
	}
	return nil
}

func validateValue(path string, value any, inCredential bool) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			credentialContext := inCredential || key == "credential"
			if scalar, ok := child.(string); ok {
				if err := validateScalar(path, key, scalar, credentialContext); err != nil {
					return err
				}
			}
			if err := validateValue(path, child, credentialContext); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validateValue(path, child, inCredential); err != nil {
				return err
			}
		}
	}
	return nil
}

func unsafePath(path string) bool {
	for _, prefix := range []string{
		".local/", "private/", "operator-policy/", "policy-runtime/", "policy-evidence/",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	for _, suffix := range []string{
		".policy.yaml", ".policy.yml", ".policy-bundle.json", ".signed-policy.json",
		".policy-approval.json", ".policy-receipt.json", ".policy-fingerprint",
	} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func validateNode(path string, node *yaml.Node, inCredential bool) error {
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index].Value
			value := node.Content[index+1]
			credentialContext := inCredential || key == "credential"
			if value.Kind == yaml.ScalarNode {
				if err := validateScalar(path, key, value.Value, credentialContext); err != nil {
					return err
				}
			}
			if err := validateNode(path, value, credentialContext); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range node.Content {
		if err := validateNode(path, child, inCredential); err != nil {
			return err
		}
	}
	return nil
}

func validateScalar(path, key, value string, inCredential bool) error {
	switch key {
	case "schema":
		if value == "hexroute.operator-policy.v1" && !strings.HasPrefix(path, "testdata/policy/") {
			return unsafe(path, "operator policy outside synthetic fixtures")
		}
		if _, forbidden := forbiddenPolicySchemas[value]; forbidden {
			return unsafe(path, "compiled or signed policy artifact")
		}
	case "signer_fingerprint", "trust_fingerprint", "pinned_public_key", "signature",
		"credential_value", "private_key":
		if value != "" {
			return unsafe(path, "trust or secret material")
		}
	case "source_path":
		if value != "" {
			return unsafe(path, "live source path")
		}
	case "profile_id":
		if value != "" && !strings.HasPrefix(value, "synthetic-") {
			return unsafe(path, "non-synthetic profile identifier")
		}
	case "credential_reference", "credential_ref":
		if !syntheticReference(value) {
			return unsafe(path, "non-synthetic credential reference")
		}
	case "reference":
		if inCredential && !syntheticReference(value) {
			return unsafe(path, "non-synthetic credential reference")
		}
	case "host", "hostname", "server_name":
		if value != "" && !syntheticHost(value) {
			return unsafe(path, "non-synthetic host selector")
		}
	case "address", "proxy_address", "managed_tun_address", "upstream_probe_address":
		if value != "" && !syntheticAddress(value) {
			return unsafe(path, "non-synthetic address selector")
		}
	case "prefix", "cidr":
		if value != "" && !syntheticPrefix(value) {
			return unsafe(path, "non-synthetic route selector")
		}
	}
	return nil
}

func syntheticReference(value string) bool {
	return value != "" && (strings.HasPrefix(value, "synthetic-") || strings.HasPrefix(value, "HEXROUTE_CANARY_"))
}

func syntheticHost(value string) bool {
	host := strings.ToLower(strings.TrimSuffix(value, "."))
	if host == "localhost" || strings.HasSuffix(host, ".example") || strings.HasSuffix(host, ".invalid") {
		return true
	}
	if address, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return syntheticIP(address)
	}
	return false
}

func syntheticAddress(value string) bool {
	host := value
	if split, _, err := net.SplitHostPort(value); err == nil {
		host = split
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && syntheticIP(address)
}

func syntheticPrefix(value string) bool {
	prefix, err := netip.ParsePrefix(value)
	return err == nil && syntheticIP(prefix.Addr())
}

func syntheticIP(address netip.Addr) bool {
	if address.IsLoopback() {
		return true
	}
	for _, prefix := range documentationPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func unsafe(path, reason string) error {
	return fmt.Errorf("%w: %s: %s", ErrUnsafeArtifact, path, reason)
}
