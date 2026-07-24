package operator

import (
	"context"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

type Envelope struct {
	Request ipc.Request
	reply   chan ipc.Response
	ctx     context.Context
}

type Broker struct {
	ctx      context.Context
	requests chan Envelope
}

func NewBroker(ctx context.Context) (*Broker, error) {
	if ctx == nil {
		return nil, ErrInvalidController
	}
	return &Broker{
		ctx:      ctx,
		requests: make(chan Envelope),
	}, nil
}

func (broker *Broker) Requests() <-chan Envelope {
	if broker == nil {
		return nil
	}
	return broker.requests
}

func (broker *Broker) Handle(request ipc.Request) ipc.Response {
	return broker.HandleIPC(broker.ctx, request)
}

func (broker *Broker) HandleIPC(ctx context.Context, request ipc.Request) ipc.Response {
	if broker == nil || broker.ctx == nil {
		return internalResponse(request)
	}
	if ctx == nil {
		return internalResponse(request)
	}
	envelope := Envelope{
		Request: request,
		reply:   make(chan ipc.Response, 1),
		ctx:     ctx,
	}
	select {
	case broker.requests <- envelope:
	case <-ctx.Done():
		return internalResponse(request)
	case <-broker.ctx.Done():
		return internalResponse(request)
	}
	select {
	case response := <-envelope.reply:
		return response
	case <-ctx.Done():
		return internalResponse(request)
	case <-broker.ctx.Done():
		return internalResponse(request)
	}
}

func (envelope Envelope) Respond(response ipc.Response) bool {
	if envelope.reply == nil ||
		response.RequestID != envelope.Request.RequestID {
		return false
	}
	select {
	case envelope.reply <- response:
		return true
	default:
		return false
	}
}

func (envelope Envelope) Active() bool {
	if envelope.ctx == nil {
		return false
	}
	select {
	case <-envelope.ctx.Done():
		return false
	default:
		return true
	}
}

func internalResponse(request ipc.Request) ipc.Response {
	return ipc.Response{
		Version:   ipc.ProtocolVersion,
		RequestID: request.RequestID,
		Error:     ipc.ErrorInternal,
	}
}
