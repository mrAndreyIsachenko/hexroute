package configshapeguard_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/configshapeguard"
	"github.com/mrAndreyIsachenko/hexroute/internal/rootdaemon"
	"github.com/mrAndreyIsachenko/hexroute/internal/sentinel"
	"github.com/mrAndreyIsachenko/hexroute/internal/userdaemon"
)

// domain is one runtime: the wire type its decoder accepts, the example that
// records that type's shape, and the decoder itself.
type domain struct {
	name    string
	wire    reflect.Type
	example string
	decode  func(io.Reader) error
}

func domains() []domain {
	return []domain{
		{
			name:    "root",
			wire:    reflect.TypeOf(rootdaemon.Config{}),
			example: "../../deploy/macos/root-observe.example.json",
			decode: func(reader io.Reader) error {
				_, err := rootdaemon.DecodeConfig(reader)
				return err
			},
		},
		{
			name:    "user",
			wire:    reflect.TypeOf(userdaemon.Config{}),
			example: "../../deploy/macos/user-observe.example.json",
			decode: func(reader io.Reader) error {
				_, err := userdaemon.DecodeConfig(reader)
				return err
			},
		},
		{
			name:    "sentinel",
			wire:    reflect.TypeOf(sentinel.Config{}),
			example: "../../deploy/macos/sentinel-observe.example.json",
			decode: func(reader io.Reader) error {
				_, err := sentinel.DecodeConfig(reader)
				return err
			},
		},
	}
}

func read(t *testing.T, path string) any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("%s is not json: %v", path, err)
	}
	return document
}

// The gate. An example that omits a setting its decoder accepts is a
// configuration recorded nowhere, which is how a whole tunnel executor came to
// exist on one machine only.
func TestEveryExampleRecordsItsDecoderShape(t *testing.T) {
	for _, each := range domains() {
		t.Run(each.name, func(t *testing.T) {
			settings, err := configshapeguard.Settings(each.wire)
			if err != nil {
				t.Fatalf("Settings(%s): %v", each.wire, err)
			}
			keys, err := configshapeguard.DocumentKeys(read(t, each.example))
			if err != nil {
				t.Fatalf("DocumentKeys(%s): %v", each.example, err)
			}
			missing, extra := configshapeguard.Compare(settings, keys)
			if len(missing) > 0 {
				t.Errorf("the example omits %d settings its decoder accepts:\n    %s",
					len(missing), strings.Join(missing, "\n    "))
			}
			if len(extra) > 0 {
				t.Errorf("the example carries %d settings the decoder does not accept:\n    %s",
					len(extra), strings.Join(extra, "\n    "))
			}
			if len(settings) == 0 {
				t.Fatal("the walk found no settings, which is a broken query rather than an empty type")
			}
		})
	}
}

// The record is a template, not an inventory: the decoder reads its own
// example once the trust material a deployment supplies is supplied.
//
// The repository must not hold that material at all —
// internal/repositoryguard refuses a tracked artifact whose
// `pinned_public_key` or `signer_fingerprint` is anything but empty — so the
// example carries those keys empty and this test fills them in memory. A zero
// key with its own true digest satisfies the decoder's cross-check, which
// requires SHA256Hex(key) == fingerprint, and is no one's key.
func TestEveryExampleIsReadByItsOwnDecoderOnceTrustIsSupplied(t *testing.T) {
	for _, each := range domains() {
		t.Run(each.name, func(t *testing.T) {
			document := read(t, each.example)
			filled, supplied := supplyTrust(document)
			raw, err := json.Marshal(filled)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if err := each.decode(bytes.NewReader(raw)); err != nil {
				t.Fatalf("the decoder refused its own example (trust supplied: %v): %v",
					supplied, err)
			}
		})
	}
}

// supplyTrust fills the trust material the repository is not allowed to hold.
// It reports whether it found any, so an example that carries no policy block
// cannot pass this test by having nothing to fill.
func supplyTrust(document any) (any, bool) {
	root, ok := document.(map[string]any)
	if !ok {
		return document, false
	}
	control, ok := root["policy_control"].(map[string]any)
	if !ok {
		return document, false
	}
	key := make([]byte, ed25519.PublicKeySize)
	digest := sha256.Sum256(key)
	control["pinned_public_key"] = base64.StdEncoding.EncodeToString(key)
	control["signer_fingerprint"] = hex.EncodeToString(digest[:])
	return root, true
}

// And the repository's own boundary, stated here so the two cannot drift: the
// example holds the key and never the value.
//
// What is expected comes from the decoder, not from what the example happens to
// carry, so a domain with no policy block is checked rather than skipped: it
// must not carry one either.
func TestTheExamplesHoldNoTrustMaterial(t *testing.T) {
	trust := []string{"policy_control.pinned_public_key", "policy_control.signer_fingerprint"}
	for _, each := range domains() {
		t.Run(each.name, func(t *testing.T) {
			settings, err := configshapeguard.Settings(each.wire)
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			accepted := map[string]bool{}
			for _, setting := range settings {
				accepted[setting] = true
			}
			root, ok := read(t, each.example).(map[string]any)
			if !ok {
				t.Fatal("the example is not an object")
			}
			control, hasControl := root["policy_control"].(map[string]any)

			for _, path := range trust {
				name := strings.TrimPrefix(path, "policy_control.")
				if !accepted[path] {
					// The decoder has no such setting, so neither may the example.
					if hasControl {
						if _, present := control[name]; present {
							t.Errorf("the example carries %q, which this decoder does not accept", path)
						}
					}
					continue
				}
				if !hasControl {
					t.Errorf("the decoder accepts %q and the example carries no policy block", path)
					continue
				}
				value, present := control[name]
				if !present {
					t.Errorf("%s is absent; the key records the shape and must be there", name)
					continue
				}
				if value != "" {
					t.Errorf("%s carries a value; the repository holds the shape, not the material", name)
				}
			}
		})
	}
}

func TestTheWalkReachesASettingNestedThreeDeep(t *testing.T) {
	settings, err := configshapeguard.Settings(reflect.TypeOf(rootdaemon.Config{}))
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	wanted := map[string]bool{
		"tunnel_supervision.execution.handback_payload_failures": false,
		"tunnel_supervision.payload.proxy_address":               false,
		"policy_control.installed.trusted_compiler_sha256":       false,
		"routes.preferred_link":                                  false,
	}
	for _, setting := range settings {
		if _, ok := wanted[setting]; ok {
			wanted[setting] = true
		}
	}
	for setting, found := range wanted {
		if !found {
			t.Errorf("the walk did not reach %q", setting)
		}
	}
}

func TestASettingTheExampleOmitsIsNamed(t *testing.T) {
	settings, err := configshapeguard.Settings(reflect.TypeOf(rootdaemon.Config{}))
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	document := read(t, "../../deploy/macos/root-observe.example.json")
	delete(document.(map[string]any), "tunnel_supervision")
	keys, err := configshapeguard.DocumentKeys(document)
	if err != nil {
		t.Fatalf("DocumentKeys: %v", err)
	}
	missing, extra := configshapeguard.Compare(settings, keys)
	if len(extra) != 0 {
		t.Fatalf("removing a setting produced extras: %v", extra)
	}
	if len(missing) == 0 {
		t.Fatal("a removed block was not reported missing")
	}
	named := false
	for _, setting := range missing {
		if setting == "tunnel_supervision.execution.target_key" {
			named = true
		}
	}
	if !named {
		t.Fatalf("the nested settings of the removed block were not named: %v", missing)
	}
}

func TestASettingTheDecoderDoesNotAcceptIsNamed(t *testing.T) {
	settings, err := configshapeguard.Settings(reflect.TypeOf(rootdaemon.Config{}))
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	document := read(t, "../../deploy/macos/root-observe.example.json")
	document.(map[string]any)["a_setting_no_decoder_has"] = 1
	keys, err := configshapeguard.DocumentKeys(document)
	if err != nil {
		t.Fatalf("DocumentKeys: %v", err)
	}
	missing, extra := configshapeguard.Compare(settings, keys)
	if len(missing) != 0 {
		t.Fatalf("adding a setting produced missing ones: %v", missing)
	}
	if len(extra) != 1 || extra[0] != "a_setting_no_decoder_has" {
		t.Fatalf("extra = %v", extra)
	}
}

type unwalkable struct {
	Anything any `json:"anything"`
}

func TestAFieldTheWalkCannotEnterIsRefused(t *testing.T) {
	_, err := configshapeguard.Settings(reflect.TypeOf(unwalkable{}))
	if !errors.Is(err, configshapeguard.ErrUnresolvable) {
		t.Fatalf("err = %v, wanted ErrUnresolvable", err)
	}
}

type recursive struct {
	Name  string     `json:"name"`
	Child *recursive `json:"child"`
}

func TestATypeThatReachesItselfIsRefused(t *testing.T) {
	_, err := configshapeguard.Settings(reflect.TypeOf(recursive{}))
	if !errors.Is(err, configshapeguard.ErrCycle) {
		t.Fatalf("err = %v, wanted ErrCycle", err)
	}
}

func TestAListWithNoElementIsRefused(t *testing.T) {
	document := map[string]any{"routes": []any{}}
	_, err := configshapeguard.DocumentKeys(document)
	if !errors.Is(err, configshapeguard.ErrEmptyList) {
		t.Fatalf("err = %v, wanted ErrEmptyList", err)
	}
}

// The gate compares names. If it compared values it would become a reason to
// write a live one into the repository.
func TestTheComparisonIgnoresEveryValue(t *testing.T) {
	settings, err := configshapeguard.Settings(reflect.TypeOf(rootdaemon.Config{}))
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	document := read(t, "../../deploy/macos/root-observe.example.json")
	replaced := replaceValues(document)
	keys, err := configshapeguard.DocumentKeys(replaced)
	if err != nil {
		t.Fatalf("DocumentKeys: %v", err)
	}
	missing, extra := configshapeguard.Compare(settings, keys)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("changing every value changed the answer: missing=%v extra=%v", missing, extra)
	}
}

func replaceValues(node any) any {
	switch typed := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for name, value := range typed {
			out[name] = replaceValues(value)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, replaceValues(value))
		}
		return out
	case string:
		return "a different string"
	case float64:
		return typed + 1
	case bool:
		return !typed
	default:
		return node
	}
}

// A field the decoder is told to ignore is not a setting. No wire type has one
// today, which is why a mutation that counted them survived the first run.
type withIgnored struct {
	Kept    string `json:"kept"`
	Ignored string `json:"-"`
	Nested  struct {
		Also    string `json:"also"`
		Skipped string `json:"-"`
	} `json:"nested"`
}

func TestAFieldTheDecoderIgnoresIsNotASetting(t *testing.T) {
	settings, err := configshapeguard.Settings(reflect.TypeOf(withIgnored{}))
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	wanted := []string{"kept", "nested", "nested.also"}
	if len(settings) != len(wanted) {
		t.Fatalf("settings = %v, wanted %v", settings, wanted)
	}
	for index, setting := range wanted {
		if settings[index] != setting {
			t.Fatalf("settings = %v, wanted %v", settings, wanted)
		}
	}
	for _, setting := range settings {
		if strings.Contains(setting, "-") || strings.HasSuffix(setting, ".") {
			t.Fatalf("an ignored field became a setting: %q", setting)
		}
	}
}

// Compare takes a key with a list index as the path without it. Its own
// producer never emits one, so nothing else reaches this.
func TestComparisonTakesAnIndexedKeyAsItsPath(t *testing.T) {
	settings := []string{"routes", "routes.name", "routes.role"}
	indexed := []string{"routes", "routes[0].name", "routes[1].name", "routes[0].role"}
	missing, extra := configshapeguard.Compare(settings, indexed)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("indexed keys were not taken as their paths: missing=%v extra=%v", missing, extra)
	}
	// And a genuinely unknown indexed key is still reported, as its path.
	missing, extra = configshapeguard.Compare(settings, append(indexed, "routes[0].nothing"))
	if len(missing) != 0 {
		t.Fatalf("missing = %v", missing)
	}
	if len(extra) != 1 || extra[0] != "routes.nothing" {
		t.Fatalf("extra = %v", extra)
	}
}
