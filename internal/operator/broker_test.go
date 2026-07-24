package operator

import (
	"context"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

func TestBrokerSerializesRequestAndResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	broker, err := NewBroker(ctx)
	if err != nil {
		t.Fatalf("NewBroker() error: %v", err)
	}
	request := ipc.Request{
		Version:   ipc.ProtocolVersion,
		RequestID: "broker-1",
		Action:    ipc.ActionStatus,
	}
	done := make(chan ipc.Response, 1)
	go func() {
		done <- broker.Handle(request)
	}()

	select {
	case envelope := <-broker.Requests():
		if envelope.Request != request {
			t.Fatalf("request = %+v, want %+v", envelope.Request, request)
		}
		if !envelope.Respond(ipc.Response{
			Version:   ipc.ProtocolVersion,
			RequestID: request.RequestID,
			Error:     ipc.ErrorPrecondition,
		}) {
			t.Fatal("Respond() rejected matching response")
		}
	case <-time.After(time.Second):
		t.Fatal("request was not delivered")
	}
	select {
	case response := <-done:
		if response.Error != ipc.ErrorPrecondition {
			t.Fatalf("response = %+v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("response was not returned")
	}
}

func TestBrokerCancellationReturnsTypedInternalError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	broker, err := NewBroker(ctx)
	if err != nil {
		t.Fatalf("NewBroker() error: %v", err)
	}
	cancel()
	response := broker.Handle(ipc.Request{
		Version:   ipc.ProtocolVersion,
		RequestID: "broker-cancelled",
		Action:    ipc.ActionStatus,
	})
	if response.OK || response.Error != ipc.ErrorInternal {
		t.Fatalf("response = %+v", response)
	}
}

func TestExpiredQueuedMutationBecomesInactive(t *testing.T) {
	daemonCtx, stopDaemon := context.WithCancel(context.Background())
	defer stopDaemon()
	broker, err := NewBroker(daemonCtx)
	if err != nil {
		t.Fatalf("NewBroker() error: %v", err)
	}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	request := ipc.Request{
		Version:   ipc.ProtocolVersion,
		RequestID: "expired-mutation",
		Action:    ipc.ActionResumeTarget,
	}
	done := make(chan ipc.Response, 1)
	go func() {
		done <- broker.HandleIPC(requestCtx, request)
	}()

	var envelope Envelope
	select {
	case envelope = <-broker.Requests():
	case <-time.After(time.Second):
		t.Fatal("mutation was not queued")
	}
	cancelRequest()
	if envelope.Active() {
		t.Fatal("expired mutation remains active")
	}
	select {
	case response := <-done:
		if response.Error != ipc.ErrorInternal {
			t.Fatalf("response = %+v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("expired request did not return")
	}
}
