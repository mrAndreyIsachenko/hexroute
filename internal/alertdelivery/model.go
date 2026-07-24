package alertdelivery

import (
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/cloudincident"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	maxClaimBatch = 50
)

type Channel string

const (
	ChannelTelegram      Channel = "telegram"
	ChannelLocalMacOS    Channel = "local_macos"
	ChannelMorningDigest Channel = "morning_digest"
)

type DeliveryStatus string

const (
	StatusPending    DeliveryStatus = "pending"
	StatusDelivered  DeliveryStatus = "delivered"
	StatusFailed     DeliveryStatus = "failed"
	StatusSuppressed DeliveryStatus = "suppressed"
)

type Snapshot struct {
	IncidentID     metadata.UUID
	NodeID         metadata.UUID
	Generation     uint64
	Status         cloudincident.Status
	Severity       event.IncidentSeverity
	Category       event.IncidentCategory
	Component      control.Component
	RequiresAction bool
	TransitionedAt time.Time
}

type PlanItem struct {
	Channel        Channel
	Status         DeliveryStatus
	Actionable     bool
	NextAttemptAt  time.Time
	LastResultCode string
}

type Delivery struct {
	DeliveryID   metadata.UUID
	Snapshot     Snapshot
	Channel      Channel
	Actionable   bool
	AttemptCount uint32
}

type Completion string

const (
	CompletionDelivered   Completion = "delivered"
	CompletionUnavailable Completion = "unavailable"
)

var (
	ErrInvalidPolicy       = errors.New("invalid alert delivery policy")
	ErrInvalidDelivery     = errors.New("invalid alert delivery")
	ErrDeliveryNotFound    = errors.New("alert delivery not found")
	ErrDeliveryClaimLost   = errors.New("alert delivery claim lost")
	ErrTelegramUnavailable = errors.New("telegram delivery unavailable")
)
