// Package objectstore writes and removes private objects in an S3-compatible
// store, and does nothing else.
//
// Two methods are what internal/incidentbundle asks for, and two methods are
// what this provides. It is signed by hand rather than by adding an SDK:
// this repository has six direct dependencies and sixty lines of go.sum, and
// a PUT and a DELETE do not justify dozens of modules to carry them.
//
// What it does not do is as deliberate as what it does. It cannot list, cannot
// read an object back, and cannot reach a bucket other than the one it was
// constructed for. A bundle is evidence a person reads out of band; nothing
// here gives the cloud runtime a way to read one back in.
package objectstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	algorithm   = "AWS4-HMAC-SHA256"
	service     = "s3"
	terminator  = "aws4_request"
	longFormat  = "20060102T150405Z"
	shortFormat = "20060102"
)

// credentials are what signing needs. They are held rather than read from the
// environment at each call, so a request cannot start being signed with a
// different identity than the one the store was built with.
type credentials struct {
	accessKeyID string
	secretKey   string
	region      string
}

// sign attaches the SigV4 headers to a request whose body hash is known.
//
// The payload hash is required rather than computed here: the caller already
// has it — a bundle is addressed by the digest of its own content — and
// hashing it twice would be a second answer to a question that already has
// one.
func sign(
	request *http.Request,
	payloadHash string,
	creds credentials,
	at time.Time,
) error {
	if request == nil || request.URL == nil {
		return fmt.Errorf("%w: no request", ErrObjectStore)
	}
	if creds.accessKeyID == "" || creds.secretKey == "" || creds.region == "" {
		return fmt.Errorf("%w: incomplete credentials", ErrObjectStore)
	}

	stamp := at.UTC()
	request.Header.Set("Host", request.URL.Host)
	request.Header.Set("X-Amz-Date", stamp.Format(longFormat))
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)

	signed, canonicalHeaders := canonicalize(request.Header, request.URL.Host)
	canonicalRequest := strings.Join([]string{
		request.Method,
		canonicalPath(request.URL),
		canonicalQuery(request.URL),
		canonicalHeaders,
		signed,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{
		stamp.Format(shortFormat), creds.region, service, terminator,
	}, "/")
	stringToSign := strings.Join([]string{
		algorithm,
		stamp.Format(longFormat),
		scope,
		hex.EncodeToString(hashOf(canonicalRequest)),
	}, "\n")

	signature := hex.EncodeToString(
		mac(signingKey(creds, stamp), []byte(stringToSign)))

	request.Header.Set("Authorization", fmt.Sprintf(
		"%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, creds.accessKeyID, scope, signed, signature))
	return nil
}

// signingKey derives the key for one day, region and service. Every input is
// folded in, so a key cannot be reused across any of them.
func signingKey(creds credentials, at time.Time) []byte {
	key := mac([]byte("AWS4"+creds.secretKey), []byte(at.Format(shortFormat)))
	key = mac(key, []byte(creds.region))
	key = mac(key, []byte(service))
	return mac(key, []byte(terminator))
}

func mac(key, message []byte) []byte {
	writer := hmac.New(sha256.New, key)
	writer.Write(message)
	return writer.Sum(nil)
}

func hashOf(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

// canonicalize returns the signed header list and the canonical header block.
//
// Only host and the x-amz headers are signed. Signing everything a transport
// might add — a proxy's headers, a redirect's — makes a signature that fails
// for reasons the caller cannot see.
func canonicalize(header http.Header, host string) (string, string) {
	names := []string{"host"}
	values := map[string]string{"host": host}
	for name, list := range header {
		lower := strings.ToLower(name)
		if !strings.HasPrefix(lower, "x-amz-") {
			continue
		}
		names = append(names, lower)
		values[lower] = strings.TrimSpace(strings.Join(list, ","))
	}
	sort.Strings(names)

	var block strings.Builder
	for _, name := range names {
		block.WriteString(name)
		block.WriteString(":")
		block.WriteString(values[name])
		block.WriteString("\n")
	}
	return strings.Join(names, ";"), block.String()
}

// canonicalPath escapes each segment but keeps the separators, which is what
// the signature is computed over.
func canonicalPath(target *url.URL) string {
	if target.Path == "" {
		return "/"
	}
	segments := strings.Split(target.Path, "/")
	for index, segment := range segments {
		segments[index] = escape(segment)
	}
	return strings.Join(segments, "/")
}

func canonicalQuery(target *url.URL) string {
	values := target.Query()
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		list := append([]string(nil), values[key]...)
		sort.Strings(list)
		for _, value := range list {
			pairs = append(pairs, escape(key)+"="+escape(value))
		}
	}
	return strings.Join(pairs, "&")
}

// escape is RFC 3986 unreserved-only, which is what SigV4 requires and what
// url.QueryEscape does not do: it encodes a space as "+" and leaves other
// characters alone.
func escape(value string) string {
	var out strings.Builder
	for _, b := range []byte(value) {
		switch {
		case (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') ||
			(b >= '0' && b <= '9') || b == '-' || b == '_' || b == '.' || b == '~':
			out.WriteByte(b)
		default:
			fmt.Fprintf(&out, "%%%02X", b)
		}
	}
	return out.String()
}

func sha256Of(content []byte) [32]byte {
	return sha256.Sum256(content)
}
