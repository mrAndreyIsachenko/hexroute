package telemetry

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
	"github.com/mrAndreyIsachenko/hexroute/internal/spool"
)

type Transport interface {
	Upload(
		context.Context,
		signing.SignedEnvelope,
		[]byte,
	) (Acknowledgement, error)
}

type Uploader struct {
	mu        sync.Mutex
	journal   *spool.Spool
	key       signing.Key
	transport Transport
	random    io.Reader
	now       func() time.Time
}

var (
	ErrUploadFailed          = errors.New("telemetry upload failed")
	ErrUploaderMisconfigured = errors.New("telemetry uploader is misconfigured")
)

func NewUploader(
	journal *spool.Spool,
	key signing.Key,
	transport Transport,
	random io.Reader,
	now func() time.Time,
) (*Uploader, error) {
	if journal == nil || transport == nil || len(key.PublicKey()) == 0 {
		return nil, ErrUploaderMisconfigured
	}
	if now == nil {
		now = time.Now
	}
	return &Uploader{
		journal:   journal,
		key:       key,
		transport: transport,
		random:    random,
		now:       now,
	}, nil
}

func (uploader *Uploader) RunOnce(ctx context.Context) error {
	uploader.mu.Lock()
	defer uploader.mu.Unlock()

	entries, err := uploader.journal.Entries()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	if len(entries) > MaxBatchEvents {
		entries = entries[:MaxBatchEvents]
	}

	batchID, err := metadata.NewUUID(uploader.random)
	if err != nil {
		return err
	}
	requestID, err := metadata.NewUUID(uploader.random)
	if err != nil {
		return err
	}
	body, err := EncodeBatch(batchID, entries)
	if err != nil {
		return err
	}
	signed, err := signing.Sign(uploader.key, requestID, uploader.now(), body)
	if err != nil {
		return err
	}
	acknowledgement, err := uploader.transport.Upload(ctx, signed, body)
	if err != nil {
		return errors.Join(ErrUploadFailed, err)
	}
	_, err = ApplyAcknowledgement(
		uploader.journal,
		batchID,
		uploader.key.NodeID,
		requestID,
		acknowledgement,
	)
	return err
}
