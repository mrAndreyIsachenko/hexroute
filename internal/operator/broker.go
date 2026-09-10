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
	refusals RefusalReporter
}

func NewBroker(ctx context.Context, refusals RefusalReporter) (*Broker, error) {
	if ctx == nil || refusals == nil {
		return nil, ErrInvalidController
	}
	return &Broker{
		ctx:      ctx,
		requests: make(chan Envelope),
		refusals: refusals,
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
		return broker.unanswered(request)
	case <-broker.ctx.Done():
		return broker.unanswered(request)
	}
	select {
	case response := <-envelope.reply:
		return response
	case <-ctx.Done():
		return broker.unanswered(request)
	case <-broker.ctx.Done():
		return broker.unanswered(request)
	}
}

// unanswered records that nothing took this request up, and answers.
//
// It is not a refusal: nothing looked at the request and disagreed with it.
// The distinction matters to whoever reads the answer, because a refusal sends
// them to the policy and this sends them to whatever is keeping the reader
// busy. The code on the wire cannot carry it, so it is written down here.
func (broker *Broker) unanswered(request ipc.Request) ipc.Response {
	if broker != nil && broker.refusals != nil {
		broker.refusals.ReportRequestUnanswered()
	}
	return internalResponse(request)
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
