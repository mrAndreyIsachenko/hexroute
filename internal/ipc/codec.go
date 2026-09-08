package ipc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
)

var (
	ErrFrameTooLarge  = errors.New("IPC frame exceeds maximum size")
	ErrMalformedFrame = errors.New("malformed IPC frame")
	// ErrPeerSilent is a peer that did not finish answering in time.
	//
	// It is not a malformed frame, though it arrives here as the same failed
	// read. Calling it one is how a deadline the caller set itself came back
	// as the other side's fault: a publisher that gives up after five seconds
	// while the server is still allowed fifteen produces this every cycle, and
	// both logs then name the other machine.
	ErrPeerSilent = errors.New("IPC peer did not answer in time")
)

func WriteFrame(writer io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode IPC frame: %w", err)
	}
	if len(payload) > MaxFrameBytes {
		return ErrFrameTooLarge
	}

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := writer.Write(header[:]); err != nil {
		return fmt.Errorf("write IPC frame header: %w", err)
	}
	if _, err := writer.Write(payload); err != nil {
		return fmt.Errorf("write IPC frame payload: %w", err)
	}
	return nil
}

func ReadRequest(reader io.Reader) (Request, error) {
	payload, err := readFrame(reader)
	if err != nil {
		return Request{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()

	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, ErrMalformedFrame
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Request{}, ErrMalformedFrame
	}
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}

func ReadResponse(reader io.Reader) (Response, error) {
	payload, err := readFrame(reader)
	if err != nil {
		return Response{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()

	var response Response
	if err := decoder.Decode(&response); err != nil {
		return Response{}, ErrMalformedFrame
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Response{}, ErrMalformedFrame
	}
	if err := response.Validate(); err != nil {
		return Response{}, err
	}
	return response, nil
}

func readFrame(reader io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, frameReadError(err)
	}

	length := binary.BigEndian.Uint32(header[:])
	if length == 0 {
		return nil, ErrMalformedFrame
	}
	if length > MaxFrameBytes {
		return nil, ErrFrameTooLarge
	}

	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, frameReadError(err)
	}
	return payload, nil
}

// frameReadError separates a peer that answered badly from one that did not
// answer at all.
//
// A frame is malformed when its bytes are wrong. A read that timed out, or a
// connection that closed before the frame arrived, says nothing about the
// bytes: it says the exchange did not finish, which is a different fault, on a
// different side, with a different fix.
func frameReadError(err error) error {
	switch {
	case errors.Is(err, os.ErrDeadlineExceeded),
		errors.Is(err, context.DeadlineExceeded):
		return ErrPeerSilent
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, net.ErrClosed), errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.EPIPE):
		return ErrPeerSilent
	default:
		return ErrMalformedFrame
	}
}
