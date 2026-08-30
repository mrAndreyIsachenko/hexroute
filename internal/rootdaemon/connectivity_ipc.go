package rootdaemon

import (
	"context"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityhost"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

// connectivityPublisher receives what the user domain observed.
//
// It answers with counts and a watermark. There is deliberately no path from
// here back to the user daemon carrying a snapshot, a diff or a proposal: root
// hears the user domain, and does not tell it what to conclude.
type connectivityPublisher struct {
	reader *connectivityhost.Reader
}

func (publisher connectivityPublisher) Publish(
	_ context.Context,
	request ipc.Request,
) ipc.Response {
	response := ipc.Response{Version: ipc.ProtocolVersion, RequestID: request.RequestID}
	if publisher.reader == nil || request.PublishConnectivityFacts == nil {
		response.Error = ipc.ErrorPrecondition
		return response
	}
	report, err := publisher.reader.PublishUser(request.PublishConnectivityFacts.Facts)
	if err != nil {
		// A publication root could not fold is refused, not partially kept.
		// The user daemon then keeps observing and publishes again; a fact it
		// believes was accepted when it was not is the one outcome that would
		// make the two sides disagree about what is known.
		response.Error = ipc.ErrorInvalidRequest
		return response
	}
	response.OK = true
	response.PublishConnectivityFacts = &ipc.PublishConnectivityFactsResult{
		Accepted:      report.Accepted,
		Duplicates:    report.Duplicates,
		Conflicts:     report.Conflicts,
		Rejected:      report.Rejected,
		HighWatermark: report.Watermark,
	}
	return response
}
