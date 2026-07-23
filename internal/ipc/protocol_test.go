package ipc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

func TestRequestFrameRoundTrip(t *testing.T) {
	request := Request{
		Version:            ProtocolVersion,
		RequestID:          "request-01",
		Action:             ActionResumeTarget,
		Target:             control.ComponentTunnel,
		ExpectedGeneration: 7,
	}

	var frame bytes.Buffer
	if err := WriteFrame(&frame, request); err != nil {
		t.Fatalf("WriteFrame() error: %v", err)
	}
	decoded, err := ReadRequest(&frame)
	if err != nil {
		t.Fatalf("ReadRequest() error: %v", err)
	}
	if decoded != request {
		t.Fatalf("ReadRequest() = %+v, want %+v", decoded, request)
	}
}

func TestReadRequestRejectsMalformedOversizedAndArbitraryCommand(t *testing.T) {
	tests := []struct {
		name  string
		frame []byte
		want  error
	}{
		{
			name:  "malformed JSON",
			frame: rawFrame([]byte(`{"version":`)),
			want:  ErrMalformedFrame,
		},
		{
			name: "unknown command field",
			frame: rawFrame([]byte(
				`{"version":1,"request_id":"request-01","action":"status","expected_generation":0,"command":"rm -rf"}`,
			)),
			want: ErrMalformedFrame,
		},
		{
			name: "arbitrary action",
			frame: rawFrame([]byte(
				`{"version":1,"request_id":"request-01","action":"run_shell","expected_generation":0}`,
			)),
			want: ErrUnknownAction,
		},
		{
			name:  "oversized frame",
			frame: oversizedHeader(),
			want:  ErrFrameTooLarge,
		},
		{
			name: "unsupported version",
			frame: rawFrame([]byte(
				`{"version":2,"request_id":"request-01","action":"status","expected_generation":0}`,
			)),
			want: ErrUnsupportedVersion,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ReadRequest(bytes.NewReader(test.frame))
			if !errors.Is(err, test.want) {
				t.Fatalf("ReadRequest() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestAuthorizePeerUID(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "ipc.sock")
	listener, err := netListenUnix(socketPath)
	if err != nil {
		t.Fatalf("listen Unix socket: %v", err)
	}
	defer listener.Close()

	client, err := netDialUnix(socketPath)
	if err != nil {
		t.Fatalf("dial Unix socket: %v", err)
	}
	defer client.Close()

	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatalf("accept Unix socket: %v", err)
	}
	defer server.Close()

	currentUID := uint32(os.Getuid())
	if _, err := AuthorizePeer(server, currentUID); err != nil {
		t.Fatalf("AuthorizePeer(current UID) error: %v", err)
	}
	if _, err := AuthorizePeer(server, currentUID+1); !errors.Is(err, ErrUnauthorizedPeer) {
		t.Fatalf("AuthorizePeer(other UID) error = %v, want %v", err, ErrUnauthorizedPeer)
	}
}

func rawFrame(payload []byte) []byte {
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	return frame
}

func oversizedHeader() []byte {
	frame := make([]byte, 4)
	binary.BigEndian.PutUint32(frame, MaxFrameBytes+1)
	return frame
}
