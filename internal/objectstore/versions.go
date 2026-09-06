package objectstore

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// MaxVersionBytes bounds one configuration version object. It is far below the
// bundle ceiling because a configuration is a few kilobytes and a reader that
// accepted a megabyte would be paying for something no publisher can produce.
const MaxVersionBytes = 256 * 1024

// Access is what one configuration store may do.
//
// The operator publishes and the node fetches, and neither does the other. The
// two are separate credentials in separate places, and this is that separation
// written where it can be tested: a store built to fetch cannot be made to
// write by calling the other method.
type Access string

const (
	AccessPublish Access = "publish"
	AccessFetch   Access = "fetch"
)

// VersionStore carries configuration versions. It is a distinct type from the
// bundle Store on purpose: the bundle store may never read, because a bundle
// nothing can read back is evidence rather than input, and a configuration
// version is exactly the opposite — it exists to be read by the host that runs
// it.
type VersionStore struct {
	store  *Store
	access Access
}

// NewVersionStore builds a store that may do one thing.
func NewVersionStore(config Config, access Access) (*VersionStore, error) {
	if access != AccessPublish && access != AccessFetch {
		return nil, fmt.Errorf("%w: unknown access %q", ErrObjectStore, string(access))
	}
	store, err := New(config)
	if err != nil {
		return nil, err
	}
	return &VersionStore{store: store, access: access}, nil
}

// PutVersion writes one version object.
func (versions *VersionStore) PutVersion(
	ctx context.Context, key string, content []byte,
) error {
	if versions == nil || versions.store == nil {
		return fmt.Errorf("%w: no store", ErrObjectStore)
	}
	if versions.access != AccessPublish {
		return fmt.Errorf("%w: this store may only fetch", ErrObjectStore)
	}
	if key == "" {
		return fmt.Errorf("%w: no key", ErrObjectStore)
	}
	if len(content) == 0 || len(content) > MaxVersionBytes {
		return fmt.Errorf("%w: %d bytes", ErrObjectStore, len(content))
	}
	request, err := versions.store.request(ctx, http.MethodPut, key, bytes.NewReader(content))
	if err != nil {
		return err
	}
	request.ContentLength = int64(len(content))
	request.Header.Set("Content-Type", "application/json")
	// Private is the bucket's own policy; saying it again on the object means
	// a bucket misconfigured later cannot make this object public.
	request.Header.Set("X-Amz-Acl", "private")
	digest := sha256Of(content)
	return versions.store.send(request, hex.EncodeToString(digest[:]))
}

// GetVersion reads one version object, bounded.
//
// What it returns is not authentic by having been returned. The caller
// verifies these bytes against a signature and a public key it holds
// independently of this store, and that is the whole reason the node may read
// from a place it does not otherwise trust.
func (versions *VersionStore) GetVersion(
	ctx context.Context, key string,
) ([]byte, error) {
	if versions == nil || versions.store == nil {
		return nil, fmt.Errorf("%w: no store", ErrObjectStore)
	}
	if versions.access != AccessFetch {
		return nil, fmt.Errorf("%w: this store may only publish", ErrObjectStore)
	}
	if key == "" {
		return nil, fmt.Errorf("%w: no key", ErrObjectStore)
	}
	request, err := versions.store.request(ctx, http.MethodGet, key, nil)
	if err != nil {
		return nil, err
	}
	if err := sign(request, emptyPayloadHash, versions.store.creds, versions.store.now()); err != nil {
		return nil, err
	}
	response, err := versions.store.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrObjectStore, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.CopyN(io.Discard, response.Body, 8*1024)
		return nil, fmt.Errorf("%w: GET returned %s", ErrObjectStore,
			strconv.Itoa(response.StatusCode))
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, MaxVersionBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrObjectStore, err)
	}
	if len(content) == 0 || len(content) > MaxVersionBytes {
		return nil, fmt.Errorf("%w: %d bytes read", ErrObjectStore, len(content))
	}
	return content, nil
}
