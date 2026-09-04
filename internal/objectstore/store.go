package objectstore

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/incidentbundle"
)

// ErrObjectStore is any failure to write or remove an object.
var ErrObjectStore = errors.New("object store is unavailable")

const (
	// MaxObjectBytes bounds one write. It is the bundle's own compressed
	// ceiling: this store exists to carry bundles, and refusing anything
	// larger keeps a caller from discovering the limit at the far end.
	MaxObjectBytes = 1024 * 1024
	// requestTimeout bounds one call. A worker pass that hangs on a write
	// stops doing incident correlation and retention as well.
	requestTimeout = 30 * time.Second
)

// Config is what one store needs. Every field is required: a store that
// defaulted a bucket or a region would be a store that could write somewhere
// nobody chose.
type Config struct {
	Endpoint    string
	Region      string
	Bucket      string
	AccessKeyID string
	SecretKey   string
}

// Store writes and removes private objects in one bucket.
type Store struct {
	client   *http.Client
	endpoint *url.URL
	bucket   string
	creds    credentials
	now      func() time.Time
}

// New builds a store for exactly one bucket.
func New(config Config) (*Store, error) {
	if config.Endpoint == "" || config.Region == "" || config.Bucket == "" ||
		config.AccessKeyID == "" || config.SecretKey == "" {
		return nil, fmt.Errorf("%w: incomplete configuration", ErrObjectStore)
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		// Plain HTTP would put a signed request and its payload on the wire
		// in the clear, and the payload is redacted incident evidence.
		return nil, fmt.Errorf("%w: endpoint must be an https URL", ErrObjectStore)
	}
	return &Store{
		client:   &http.Client{Timeout: requestTimeout},
		endpoint: endpoint,
		bucket:   config.Bucket,
		creds: credentials{
			accessKeyID: config.AccessKeyID,
			secretKey:   config.SecretKey,
			region:      config.Region,
			service:     s3Service,
		},
		now: time.Now,
	}, nil
}

// PutPrivate writes one object.
//
// Repeating an identical write is the store's own idempotence: the same key
// and the same bytes produce the same object, which is what lets an
// interrupted bundle creation simply be retried.
func (store *Store) PutPrivate(
	ctx context.Context, object incidentbundle.PrivateObject,
) error {
	if store == nil {
		return fmt.Errorf("%w: no store", ErrObjectStore)
	}
	if object.Key == "" {
		return fmt.Errorf("%w: no key", ErrObjectStore)
	}
	if len(object.Content) == 0 || len(object.Content) > MaxObjectBytes {
		return fmt.Errorf("%w: %d bytes", ErrObjectStore, len(object.Content))
	}
	// The caller addressed the object by this digest. Writing content that
	// does not match it would store something under a name that describes
	// something else.
	if sha256Of(object.Content) != object.ContentSHA256 {
		return fmt.Errorf("%w: content does not match its digest", ErrObjectStore)
	}

	request, err := store.request(ctx, http.MethodPut, object.Key,
		bytes.NewReader(object.Content))
	if err != nil {
		return err
	}
	request.ContentLength = int64(len(object.Content))
	if object.ContentType != "" {
		request.Header.Set("Content-Type", object.ContentType)
	}
	if object.ContentEncoding != "" {
		request.Header.Set("Content-Encoding", object.ContentEncoding)
	}
	// Private is the bucket's own policy; saying it again on the object means
	// a bucket misconfigured later cannot make this object public.
	request.Header.Set("X-Amz-Acl", "private")
	if !object.ExpiresAt.IsZero() {
		request.Header.Set("X-Amz-Meta-Expires-At",
			object.ExpiresAt.UTC().Format(time.RFC3339))
	}

	return store.send(request, hex.EncodeToString(object.ContentSHA256[:]))
}

// DeletePrivate removes one object. A key that is already gone is not an
// error: expiry runs repeatedly and the lifecycle rule removes objects too.
func (store *Store) DeletePrivate(ctx context.Context, key string) error {
	if store == nil {
		return fmt.Errorf("%w: no store", ErrObjectStore)
	}
	if key == "" {
		return fmt.Errorf("%w: no key", ErrObjectStore)
	}
	request, err := store.request(ctx, http.MethodDelete, key, nil)
	if err != nil {
		return err
	}
	return store.send(request, emptyPayloadHash)
}

const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func (store *Store) request(
	ctx context.Context, method, key string, body io.Reader,
) (*http.Request, error) {
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return nil, fmt.Errorf("%w: refusing key %q", ErrObjectStore, key)
	}
	target := *store.endpoint
	target.Path = "/" + store.bucket + "/" + key
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrObjectStore, err)
	}
	return request, nil
}

func (store *Store) send(request *http.Request, payloadHash string) error {
	if err := sign(request, payloadHash, store.creds, store.now()); err != nil {
		return err
	}
	response, err := store.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrObjectStore, err)
	}
	defer response.Body.Close()
	// The body is read and discarded so the connection can be reused, and
	// bounded so a store answering with something enormous cannot cost more
	// than the write did.
	_, _ = io.CopyN(io.Discard, response.Body, 8*1024)

	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return nil
	case request.Method == http.MethodDelete && response.StatusCode == http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("%w: %s returned %s", ErrObjectStore,
			request.Method, strconv.Itoa(response.StatusCode))
	}
}
