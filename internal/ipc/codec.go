package ipc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var (
	ErrFrameTooLarge  = errors.New("IPC frame exceeds maximum size")
	ErrMalformedFrame = errors.New("malformed IPC frame")
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
	if response.Version != ProtocolVersion || !validRequestID(response.RequestID) {
		return Response{}, ErrMalformedFrame
	}
	return response, nil
}

func readFrame(reader io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, ErrMalformedFrame
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
		return nil, ErrMalformedFrame
	}
	return payload, nil
}
