package objectstore

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testVersionStore(t *testing.T, access Access, handler http.HandlerFunc) (*VersionStore, func()) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	versions, err := NewVersionStore(testConfig(), access)
	if err != nil {
		t.Fatalf("new version store: %v", err)
	}
	endpoint, _ := url.Parse(server.URL)
	versions.store.endpoint = endpoint
	versions.store.now = func() time.Time { return fixedTime }
	versions.store.client = server.Client()
	versions.store.client.Transport.(*http.Transport).TLSClientConfig =
		&tls.Config{InsecureSkipVerify: true}
	return versions, server.Close
}

// The operator publishes and the node fetches. A store that could do both
// would put the write credential wherever the read one went, which on this
// fleet is a host in someone else's data centre.
func TestAConfigurationStoreDoesOneThing(t *testing.T) {
	called := false
	fetcher, closeFetcher := testVersionStore(t, AccessFetch,
		func(http.ResponseWriter, *http.Request) { called = true })
	defer closeFetcher()
	if err := fetcher.PutVersion(context.Background(), "versions/a.json", []byte(`{}`)); err == nil {
		t.Fatal("a fetch store wrote")
	}

	publisher, closePublisher := testVersionStore(t, AccessPublish,
		func(http.ResponseWriter, *http.Request) { called = true })
	defer closePublisher()
	if _, err := publisher.GetVersion(context.Background(), "versions/a.json"); err == nil {
		t.Fatal("a publish store read")
	}
	if called {
		t.Fatal("a refused call still reached the store")
	}

	if _, err := NewVersionStore(testConfig(), Access("both")); err == nil {
		t.Fatal("an unknown access was accepted")
	}
}

func TestAPublishedVersionIsWrittenPrivateAndSigned(t *testing.T) {
	var seen *http.Request
	var body []byte
	publisher, closeServer := testVersionStore(t, AccessPublish,
		func(writer http.ResponseWriter, request *http.Request) {
			seen = request
			body = readBody(request)
			writer.WriteHeader(http.StatusOK)
		})
	defer closeServer()

	content := []byte(`{"statement":{},"signature":"x"}`)
	if err := publisher.PutVersion(context.Background(), "versions/node/a.json", content); err != nil {
		t.Fatalf("put: %v", err)
	}
	if seen == nil {
		t.Fatal("the store was never called")
	}
	if seen.Header.Get("X-Amz-Acl") != "private" {
		t.Fatal("the version was not written private")
	}
	if seen.Header.Get("Authorization") == "" {
		t.Fatal("the request went unsigned")
	}
	if !bytes.Equal(body, content) {
		t.Fatal("the stored bytes are not the ones published")
	}
}

// A fetch is bounded and its status is honoured. Neither is about trusting
// what comes back: the caller verifies a signature over these bytes.
func TestAFetchIsBoundedAndRefusesWhatIsNotThere(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{name: "a version", status: http.StatusOK, body: `{"a":1}`},
		{name: "no such version", status: http.StatusNotFound, body: "", wantErr: true},
		{name: "an empty object", status: http.StatusOK, body: "", wantErr: true},
		{
			name:    "more than a version can be",
			status:  http.StatusOK,
			body:    strings.Repeat("a", MaxVersionBytes+1),
			wantErr: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fetcher, closeServer := testVersionStore(t, AccessFetch,
				func(writer http.ResponseWriter, _ *http.Request) {
					writer.WriteHeader(testCase.status)
					_, _ = writer.Write([]byte(testCase.body))
				})
			defer closeServer()
			content, err := fetcher.GetVersion(context.Background(), "versions/node/a.json")
			if testCase.wantErr != (err != nil) {
				t.Fatalf("got %v, wantErr %v", err, testCase.wantErr)
			}
			if err == nil && string(content) != testCase.body {
				t.Fatal("the fetched bytes are not the ones served")
			}
			if err != nil && content != nil {
				t.Fatal("a failed fetch returned content")
			}
		})
	}
}

// A key that climbs out of the prefix is refused before it is signed, the same
// way the bundle store refuses one.
func TestVersionKeysThatClimbAreRefused(t *testing.T) {
	fetcher, closeServer := testVersionStore(t, AccessFetch,
		func(http.ResponseWriter, *http.Request) { t.Fatal("the store was called") })
	defer closeServer()
	for _, key := range []string{"", "../elsewhere", "versions/../../x", "/absolute"} {
		if _, err := fetcher.GetVersion(context.Background(), key); err == nil {
			t.Fatalf("key %q was accepted", key)
		}
	}
}

func readBody(request *http.Request) []byte {
	content, err := io.ReadAll(io.LimitReader(request.Body, MaxVersionBytes+1))
	if err != nil {
		return nil
	}
	return content
}
