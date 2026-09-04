package objectstore

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The suite's own published example identity. See testdata/sigv4/README.md for
// where these files came from and how the secret was confirmed.
func suiteCredentials() credentials {
	return credentials{
		accessKeyID: "AKIDEXAMPLE",
		secretKey:   "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
		region:      "us-east-1",
		service:     "service",
	}
}

func suiteStamp() time.Time {
	return time.Date(2015, time.August, 30, 12, 36, 0, 0, time.UTC)
}

// TestPublishedVectorsAreReproduced is the only test in this package that can
// fail because the signer disagrees with the specification rather than with
// itself. Every other one would pass unchanged on an implementation that is
// coherent and wrong.
//
// All three artifacts are compared, not only the signature. A signature is one
// opaque number: comparing it alone says that something is wrong without
// saying where, and the canonical request is where a hand-rolled signer
// usually goes wrong.
func TestPublishedVectorsAreReproduced(t *testing.T) {
	cases := suiteCases(t, "reproduced")
	if len(cases) != 14 {
		t.Fatalf("reproduced vectors = %d, want 14", len(cases))
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			request, body := suiteRequest(t, "reproduced", name)
			payload := sha256.Sum256(body)
			got := derive(
				request,
				hex.EncodeToString(payload[:]),
				suiteCredentials(),
				suiteStamp(),
			)
			for _, comparison := range []struct {
				stage     string
				extension string
				got       string
			}{
				{"canonical request", "creq", got.canonicalRequest},
				{"string to sign", "sts", got.stringToSign},
				{"authorization", "authz", got.authorization},
			} {
				want := suiteFile(t, "reproduced", name, comparison.extension)
				if comparison.got != want {
					t.Errorf(
						"%s\n got: %q\nwant: %q",
						comparison.stage,
						comparison.got,
						want,
					)
				}
			}
		})
	}
}

// TestInapplicableVectorsSignAHeaderThisStoreDoesNot keeps the reason those
// cases are excluded checkable. The exclusion is a narrowing — only host and
// x-amz-* are signed — and a narrowing that stops being true silently is how a
// signer starts covering headers a proxy adds. If any of these ever begins to
// reproduce, either the narrowing was removed or the case was misfiled, and
// both are worth stopping for.
func TestInapplicableVectorsSignAHeaderThisStoreDoesNot(t *testing.T) {
	cases := suiteCases(t, "inapplicable")
	if len(cases) != 8 {
		t.Fatalf("inapplicable vectors = %d, want 8", len(cases))
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			canonical := suiteFile(t, "inapplicable", name, "creq")
			lines := strings.Split(canonical, "\n")
			if len(lines) < 2 {
				t.Fatalf("canonical request has %d lines", len(lines))
			}
			signedHeaders := lines[len(lines)-2]
			foreign := ""
			for _, header := range strings.Split(signedHeaders, ";") {
				if header != "host" && !strings.HasPrefix(header, "x-amz-") {
					foreign = header
					break
				}
			}
			if foreign == "" {
				t.Fatalf(
					"signs only host and x-amz-* (%q) yet is filed as "+
						"inapplicable",
					signedHeaders,
				)
			}

			request, body := suiteRequest(t, "inapplicable", name)
			payload := sha256.Sum256(body)
			got := derive(
				request,
				hex.EncodeToString(payload[:]),
				suiteCredentials(),
				suiteStamp(),
			)
			if got.canonicalRequest == canonical {
				t.Fatalf(
					"reproduced a vector that signs %q; the narrowing to "+
						"host and x-amz-* is gone",
					foreign,
				)
			}
		})
	}
}

func suiteCases(t *testing.T, group string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("testdata", "sigv4", group))
	if err != nil {
		t.Fatalf("read %s vectors: %v", group, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

func suiteFile(t *testing.T, group, name, extension string) string {
	t.Helper()
	path := filepath.Join("testdata", "sigv4", group, name, name+"."+extension)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(content) == 0 {
		t.Fatalf("%s is empty", path)
	}
	return string(content)
}

// suiteRequest rebuilds the request a case describes. The host line becomes the
// URL's host rather than a header, which is where this signer reads it from.
func suiteRequest(t *testing.T, group, name string) (*http.Request, []byte) {
	t.Helper()
	raw := suiteFile(t, group, name, "req")
	reader := bufio.NewReader(strings.NewReader(raw))
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		t.Fatalf("%s has no request line", name)
	}
	fields := strings.Fields(strings.TrimRight(line, "\r\n"))
	if len(fields) < 2 {
		t.Fatalf("%s request line = %q", name, line)
	}
	header := http.Header{}
	host := ""
	for {
		next, readErr := reader.ReadString('\n')
		trimmed := strings.TrimRight(next, "\r\n")
		if trimmed == "" {
			break
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			break
		}
		if strings.EqualFold(parts[0], "host") {
			host = parts[1]
		} else {
			header.Add(parts[0], parts[1])
		}
		if readErr != nil {
			break
		}
	}
	if host == "" {
		t.Fatalf("%s names no host", name)
	}
	body, _ := reader.ReadString(0)
	target, err := url.Parse("https://" + host + fields[1])
	if err != nil {
		t.Fatalf("%s target: %v", name, err)
	}
	return &http.Request{
		Method: fields[0],
		URL:    target,
		Header: header,
	}, []byte(body)
}
