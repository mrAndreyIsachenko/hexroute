package objectstore

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/incidentbundle"
)

var fixedTime = time.Date(2026, time.September, 4, 1, 2, 3, 0, time.UTC)

func testConfig() Config {
	return Config{
		Endpoint:    "https://example.invalid",
		Region:      "fra1",
		Bucket:      "test-bucket",
		AccessKeyID: "EXAMPLEKEYIDENTIFIER",
		SecretKey:   "not-a-secret-only-a-test-fixture-value",
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(testConfig())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	store.now = func() time.Time { return fixedTime }
	return store
}

func object(content []byte) incidentbundle.PrivateObject {
	return incidentbundle.PrivateObject{
		Key:             "bundles/abc123",
		Content:         content,
		ContentSHA256:   sha256.Sum256(content),
		ContentType:     "application/json",
		ContentEncoding: "gzip",
		ExpiresAt:       fixedTime.Add(30 * 24 * time.Hour),
	}
}

// A signature that did not change when an input did would be a signature that
// does not bind that input, and every one of these is something an attacker
// or a misconfiguration could vary.
func TestEveryInputBindsTheSignature(t *testing.T) {
	base := signatureFor(t, testConfig(), http.MethodPut, "bundles/abc", "payload", fixedTime)

	for _, variation := range []struct {
		name   string
		mutate func(*Config, *string, *string, *string, *time.Time)
	}{
		{"secret", func(c *Config, _, _, _ *string, _ *time.Time) { c.SecretKey += "x" }},
		{"region", func(c *Config, _, _, _ *string, _ *time.Time) { c.Region = "ams3" }},
		{"bucket", func(c *Config, _, _, _ *string, _ *time.Time) { c.Bucket = "other" }},
		{"method", func(_ *Config, m, _, _ *string, _ *time.Time) { *m = http.MethodDelete }},
		{"key", func(_ *Config, _, k, _ *string, _ *time.Time) { *k = "bundles/other" }},
		{"payload", func(_ *Config, _, _, p *string, _ *time.Time) { *p = "different" }},
		{"date", func(_ *Config, _, _, _ *string, at *time.Time) { *at = fixedTime.Add(48 * time.Hour) }},
	} {
		t.Run(variation.name, func(t *testing.T) {
			config := testConfig()
			method, key, payload, at := http.MethodPut, "bundles/abc", "payload", fixedTime
			variation.mutate(&config, &method, &key, &payload, &at)
			if got := signatureFor(t, config, method, key, payload, at); got == base {
				t.Fatalf("changing the %s left the signature unchanged", variation.name)
			}
		})
	}
}

// The access key identifier deliberately does not bind the signature: SigV4
// carries it in Credential and signs with the secret alone, so the store can
// find which secret to verify against before it verifies. Asserting otherwise
// is asserting something untrue about the protocol, which is what the first
// version of the table above did.
func TestTheAccessKeyIdentifiesWithoutSigning(t *testing.T) {
	config := testConfig()
	first := signatureFor(t, config, http.MethodPut, "bundles/abc", "payload", fixedTime)
	config.AccessKeyID = "SECONDKEYIDENTIFIER"
	second := signatureFor(t, config, http.MethodPut, "bundles/abc", "payload", fixedTime)
	if first != second {
		t.Fatal("the access key identifier changed the signature; SigV4 signs " +
			"with the secret alone")
	}

	store, err := New(config)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	request, err := store.request(context.Background(), http.MethodPut, "x", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := sign(request, emptyPayloadHash, store.creds, fixedTime); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !strings.Contains(request.Header.Get("Authorization"),
		"Credential=SECONDKEYIDENTIFIER/") {
		t.Fatal("the access key identifier is not carried in Credential")
	}
}

func signatureFor(
	t *testing.T, config Config, method, key, payload string, at time.Time,
) string {
	t.Helper()
	store, err := New(config)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	target, _ := url.Parse(config.Endpoint + "/" + config.Bucket + "/" + key)
	request, err := http.NewRequest(method, target.String(), nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	sum := sha256.Sum256([]byte(payload))
	if err := sign(request, hexOf(sum), store.creds, at); err != nil {
		t.Fatalf("sign: %v", err)
	}
	header := request.Header.Get("Authorization")
	index := strings.Index(header, "Signature=")
	if index < 0 {
		t.Fatalf("no signature in %q", header)
	}
	return header[index:]
}

func hexOf(sum [32]byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 64)
	for _, b := range sum {
		out = append(out, digits[b>>4], digits[b&0x0f])
	}
	return string(out)
}

// The Authorization header has a shape a store parses. Getting it structurally
// wrong fails at the far end with a message that says only "signature does not
// match", which is the least informative failure there is.
func TestTheAuthorizationHeaderHasItsRequiredShape(t *testing.T) {
	store := testStore(t)
	request, err := store.request(context.Background(), http.MethodPut, "bundles/x", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := sign(request, emptyPayloadHash, store.creds, fixedTime); err != nil {
		t.Fatalf("sign: %v", err)
	}
	header := request.Header.Get("Authorization")
	for _, part := range []string{
		"AWS4-HMAC-SHA256 ",
		"Credential=EXAMPLEKEYIDENTIFIER/20260904/fra1/s3/aws4_request,",
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date,",
		"Signature=",
	} {
		if !strings.Contains(header, part) {
			t.Fatalf("header %q is missing %q", header, part)
		}
	}
	if request.Header.Get("X-Amz-Date") != "20260904T010203Z" {
		t.Fatalf("date header is %q", request.Header.Get("X-Amz-Date"))
	}
}

// SigV4 escapes to RFC 3986 unreserved. url.QueryEscape does not: it writes a
// space as "+", which produces a signature the store computes differently.
func TestEscapingIsUnreservedOnly(t *testing.T) {
	for value, want := range map[string]string{
		"plain":  "plain",
		"a b":    "a%20b",
		"a+b":    "a%2Bb",
		"a/b":    "a%2Fb",
		"a~_-.b": "a~_-.b",
		"ключ":   "%D0%BA%D0%BB%D1%8E%D1%87",
		"a=b&c":  "a%3Db%26c",
	} {
		if got := escape(value); got != want {
			t.Fatalf("escape(%q) = %q, want %q", value, got, want)
		}
	}
}

// Only host and the x-amz headers are signed. Signing whatever a transport
// adds makes a signature that fails for reasons the caller cannot see.
func TestOnlyHostAndAmzHeadersAreSigned(t *testing.T) {
	header := http.Header{}
	header.Set("X-Amz-Date", "20260904T010203Z")
	header.Set("X-Amz-Content-Sha256", emptyPayloadHash)
	header.Set("Content-Type", "application/json")
	header.Set("User-Agent", "whatever")
	signed, block := canonicalize(header, "example.invalid")
	if signed != "host;x-amz-content-sha256;x-amz-date" {
		t.Fatalf("signed headers are %q", signed)
	}
	if strings.Contains(block, "content-type") || strings.Contains(block, "user-agent") {
		t.Fatalf("the canonical block carries an unsigned header:\n%s", block)
	}
}

// Content that does not match its digest would be stored under a name that
// describes something else.
func TestContentMustMatchItsDigest(t *testing.T) {
	store := testStore(t)
	wrong := object([]byte("one"))
	wrong.Content = []byte("another")
	if err := store.PutPrivate(context.Background(), wrong); !errors.Is(err, ErrObjectStore) {
		t.Fatalf("got %v, want %v", err, ErrObjectStore)
	}
}

// The payload is redacted incident evidence. Plain HTTP would put it and its
// signature on the wire in the clear.
func TestPlainHTTPIsRefused(t *testing.T) {
	config := testConfig()
	config.Endpoint = "http://example.invalid"
	if _, err := New(config); !errors.Is(err, ErrObjectStore) {
		t.Fatalf("got %v, want %v", err, ErrObjectStore)
	}
}

// Every field is required. A store that defaulted a bucket or a region could
// write somewhere nobody chose.
func TestEveryConfigurationFieldIsRequired(t *testing.T) {
	for name, clear := range map[string]func(*Config){
		"endpoint":   func(c *Config) { c.Endpoint = "" },
		"region":     func(c *Config) { c.Region = "" },
		"bucket":     func(c *Config) { c.Bucket = "" },
		"access key": func(c *Config) { c.AccessKeyID = "" },
		"secret":     func(c *Config) { c.SecretKey = "" },
	} {
		config := testConfig()
		clear(&config)
		if _, err := New(config); !errors.Is(err, ErrObjectStore) {
			t.Fatalf("a configuration missing its %s was accepted", name)
		}
	}
}

// A key that climbs out of its prefix would write outside where the caller
// meant, in a bucket the runtime key can write to.
func TestKeysThatClimbAreRefused(t *testing.T) {
	store := testStore(t)
	for _, key := range []string{"../elsewhere", "bundles/../../x", "/absolute"} {
		if _, err := store.request(context.Background(), http.MethodPut, key, nil); err == nil {
			t.Fatalf("key %q was accepted", key)
		}
	}
}

// The transport behaviours, against a server that stands in for the store.
func TestTransportOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		status  int
		method  string
		wantErr bool
	}{
		{"a written object", http.StatusOK, http.MethodPut, false},
		{"a store that refused", http.StatusForbidden, http.MethodPut, true},
		{"a removed object", http.StatusNoContent, http.MethodDelete, false},
		{"an object already gone", http.StatusNotFound, http.MethodDelete, false},
		{"a missing object on write", http.StatusNotFound, http.MethodPut, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var seen *http.Request
			server := httptest.NewTLSServer(http.HandlerFunc(
				func(writer http.ResponseWriter, request *http.Request) {
					seen = request
					writer.WriteHeader(testCase.status)
				}))
			defer server.Close()

			store := testStore(t)
			endpoint, _ := url.Parse(server.URL)
			store.endpoint = endpoint
			store.client = server.Client()
			store.client.Transport.(*http.Transport).TLSClientConfig =
				&tls.Config{InsecureSkipVerify: true}

			var err error
			if testCase.method == http.MethodPut {
				err = store.PutPrivate(context.Background(), object([]byte(`{"a":1}`)))
			} else {
				err = store.DeletePrivate(context.Background(), "bundles/abc123")
			}
			if testCase.wantErr != (err != nil) {
				t.Fatalf("got %v, wantErr %v", err, testCase.wantErr)
			}
			if seen == nil {
				t.Fatal("the store was never called")
			}
			if seen.Header.Get("Authorization") == "" {
				t.Fatal("the request went unsigned")
			}
			if testCase.method == http.MethodPut {
				if seen.Header.Get("X-Amz-Acl") != "private" {
					t.Fatal("the object was not written private")
				}
				if seen.Header.Get("Content-Encoding") != "gzip" {
					t.Fatal("the content encoding was dropped")
				}
			}
		})
	}
}

// This store writes and removes. It cannot list and cannot read an object
// back, and that is what keeps a bundle out of anything the cloud runtime
// could reason from.
func TestTheStoreCannotRead(t *testing.T) {
	allowed := map[string]struct{}{"PutPrivate": {}, "DeletePrivate": {}}
	surface := reflectMethods()
	for _, name := range surface {
		if _, ok := allowed[name]; !ok {
			t.Fatalf("%s is exported; this store may only write and remove, "+
				"because a bundle nothing can read back is what keeps it "+
				"evidence rather than input", name)
		}
	}
}

func reflectTypeOfStore() reflect.Type { return reflect.TypeOf(&Store{}) }

func reflectMethods() []string {
	var names []string
	surface := reflectTypeOfStore()
	for index := 0; index < surface.NumMethod(); index++ {
		names = append(names, surface.Method(index).Name)
	}
	return names
}
