package ipc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
)

// A peer that did not answer is not a peer that answered badly.
//
// Both arrive at readFrame as a failed read, and calling both a malformed
// frame is how a deadline the caller set itself came back as the other side's
// fault. A publisher that gave up after five seconds while the server was
// still allowed fifteen logged "malformed frame" every cycle for a day, and
// the root daemon logged its own truncated write as a malformed request —
// each side naming the other, neither naming the deadline.
func TestAReadThatDidNotFinishIsNotAMalformedFrame(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		reader io.Reader
		want   error
	}{
		{
			name:   "the deadline fired before the header arrived",
			reader: failingReader{err: os.ErrDeadlineExceeded},
			want:   ErrPeerSilent,
		},
		{
			name:   "the peer closed before writing anything",
			reader: failingReader{err: io.EOF},
			want:   ErrPeerSilent,
		},
		{
			name:   "the frame stopped halfway through its payload",
			reader: bytes.NewReader(append(header(64), []byte(`{"ver`)...)),
			want:   ErrPeerSilent,
		},
		{
			// A length no frame can have is a statement about the bytes, not
			// about whether they arrived. That one stays malformed.
			name:   "the peer answered with a frame that cannot exist",
			reader: bytes.NewReader(header(0)),
			want:   ErrMalformedFrame,
		},
		{
			// A read that failed for a reason unrelated to the exchange
			// finishing says nothing about a deadline, so widening the
			// silence to cover it would name the wrong side again.
			name:   "the read failed for some other reason",
			reader: failingReader{err: errors.New("device not configured")},
			want:   ErrMalformedFrame,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := readFrame(testCase.reader)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("readFrame = %v, want %v", err, testCase.want)
			}
		})
	}
}

// The distinction has to survive the boundary the callers actually use, or it
// exists only where nobody reads it.
func TestReadResponseCarriesTheSilenceOutward(t *testing.T) {
	_, err := ReadResponse(failingReader{err: os.ErrDeadlineExceeded})
	if !errors.Is(err, ErrPeerSilent) {
		t.Fatalf("ReadResponse over a timed-out read = %v, want %v", err, ErrPeerSilent)
	}
	_, err = ReadRequest(failingReader{err: os.ErrDeadlineExceeded})
	if !errors.Is(err, ErrPeerSilent) {
		t.Fatalf("ReadRequest over a timed-out read = %v, want %v", err, ErrPeerSilent)
	}

	// And a peer that did answer, with bytes that are not a response, is still
	// telling us about its bytes. Widening the silence to cover that would
	// trade one wrong name for another.
	answered := append(header(5), []byte(`{{{{{`)...)
	if _, err := ReadResponse(bytes.NewReader(answered)); !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("ReadResponse over a bad payload = %v, want %v", err, ErrMalformedFrame)
	}
}

type failingReader struct{ err error }

func (reader failingReader) Read([]byte) (int, error) { return 0, reader.err }

func header(length uint32) []byte {
	var out [4]byte
	binary.BigEndian.PutUint32(out[:], length)
	return out[:]
}
