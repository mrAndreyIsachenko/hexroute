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
	mu               sync.Mutex
	journal          *spool.Spool
	key              signing.Key
	transport        Transport
	random           io.Reader
	now              func() time.Time
	gaps             GapUnrecoverableReporter
	gapRepairEnabled bool
}

type UploaderOption func(*Uploader)

func WithGapRepairEnabled(enabled bool) UploaderOption {
	return func(uploader *Uploader) {
		uploader.gapRepairEnabled = enabled
	}
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
	options ...UploaderOption,
) (*Uploader, error) {
	if journal == nil || transport == nil || len(key.PublicKey()) == 0 {
		return nil, ErrUploaderMisconfigured
	}
	if now == nil {
		now = time.Now
	}
	uploader := &Uploader{
		journal:          journal,
		key:              key,
		transport:        transport,
		random:           random,
		now:              now,
		gapRepairEnabled: true,
	}
	for _, option := range options {
		if option != nil {
			option(uploader)
		}
	}
	return uploader, nil
}

func (uploader *Uploader) RunOnce(ctx context.Context) error {
	uploader.mu.Lock()
	defer uploader.mu.Unlock()

	entries, err := uploader.batch()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
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
		entries,
		batchID,
		uploader.key.NodeID,
		requestID,
		acknowledgement,
	)
	if err != nil {
		return err
	}
	if !uploader.gapRepairEnabled {
		return nil
	}
	return uploader.replayMissingSequences(ctx, acknowledgement)
}

// batch is the oldest records this pass will send, and only those are read.
//
// It asked the spool for every record and then kept the first few hundred. That
// is the defect this repository has already paid for twice on other paths: a
// spool that nothing drains sits at its bound, so "every record" is eighty-odd
// thousand of them, and a pass that sends two hundred and fifty-six would have
// decoded all of them to choose. Draining the store would then have cost that
// scan once per batch, which is the same work squared.
//
// The listing costs no record, and each record is opened once because it is
// going out. A sequence that has gone since the listing is an ordinary answer:
// records leave by eviction and by acknowledgement, and a listing is a moment
// old by the time it is read.
//
// Oldest first, because the sequence is what the server's cursor advances
// along. Sending a later record before an earlier one would open a gap that the
// repair path then has to close, for nothing.
func (uploader *Uploader) batch() ([]spool.Entry, error) {
	sequences, err := uploader.journal.Sequences()
	if err != nil {
		return nil, err
	}
	if len(sequences) > MaxBatchEvents {
		sequences = sequences[:MaxBatchEvents]
	}
	entries := make([]spool.Entry, 0, len(sequences))
	for _, sequence := range sequences {
		entry, held, err := uploader.journal.Entry(sequence)
		if err != nil {
			return nil, err
		}
		if !held {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (uploader *Uploader) replayMissingSequences(
	ctx context.Context,
	acknowledgement Acknowledgement,
) error {
	if len(acknowledgement.MissingSequences) == 0 {
		return nil
	}
	batchID, err := metadata.NewUUID(uploader.random)
	if err != nil {
		return err
	}
	requestID, err := metadata.NewUUID(uploader.random)
	if err != nil {
		return err
	}
	budget := NewGapReplayBudget(MaxGapReplayBatchesPerRun)
	request, err := PrepareGapReplay(
		uploader.journal,
		uploader.key,
		acknowledgement.MissingSequences,
		batchID,
		requestID,
		uploader.now(),
		&budget,
	)
	if errors.Is(err, ErrGapEvidenceExpired) {
		_, reportErr := uploader.gaps.EmitOnce(
			uploader.journal,
			acknowledgement.MissingSequences,
		)
		return reportErr
	}
	if err != nil {
		return err
	}
	replayAcknowledgement, err := uploader.transport.Upload(
		ctx,
		request.Envelope,
		request.Body,
	)
	if err != nil {
		return errors.Join(ErrUploadFailed, err)
	}
	_, err = ApplyAcknowledgement(
		uploader.journal,
		request.Entries,
		request.BatchID,
		uploader.key.NodeID,
		request.RequestID,
		replayAcknowledgement,
	)
	return err
}
