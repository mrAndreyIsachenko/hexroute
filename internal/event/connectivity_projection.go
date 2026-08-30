package event

// The projection is what the cloud is allowed to know about host
// connectivity. It is an allowlist by construction: every field here is a
// bounded enumeration, a count of components or a generation.
//
// It deliberately has no field for an address, hostname, route prefix,
// selector, endpoint, source path, process detail, event identifier, session
// identifier, credential reference — or a proposal digest, which would let the
// cloud name a specific local decision.
//
// It also carries no payload counts. How many routes or relays a host has
// configured is topology, and the cloud has no use for it.

// MaxProjectedComponents bounds one projection.
const MaxProjectedComponents = 16

// Freshness is a bucket, never a deadline or a timestamp.
type Freshness string

const (
	FreshnessFresh         Freshness = "fresh"
	FreshnessStale         Freshness = "stale"
	FreshnessNeverObserved Freshness = "never_observed"
)

func (freshness Freshness) valid() bool {
	switch freshness {
	case FreshnessFresh, FreshnessStale, FreshnessNeverObserved:
		return true
	default:
		return false
	}
}

// ProjectedComponent is one component's state as the cloud sees it.
type ProjectedComponent struct {
	Component string    `json:"component"`
	State     string    `json:"state"`
	Freshness Freshness `json:"freshness"`
	Reason    string    `json:"reason"`
}

// ProjectedProposalClass counts proposals of one class without naming any.
type ProjectedProposalClass struct {
	Class string `json:"class"`
	Count uint16 `json:"count"`
}

// ConnectivityProjection is the redacted connectivity snapshot.
type ConnectivityProjection struct {
	SnapshotGeneration uint64 `json:"snapshot_generation"`
	ReducerVersion     uint16 `json:"reducer_version"`

	BundleGeneration uint64 `json:"bundle_generation"`
	RootGeneration   uint64 `json:"root_generation"`
	UserGeneration   uint64 `json:"user_generation"`

	Aggregate           string `json:"aggregate"`
	Authorization       string `json:"authorization"`
	AuthorizationReason string `json:"authorization_reason"`

	Components []ProjectedComponent `json:"components"`

	OpenGaps        uint16 `json:"open_gaps"`
	GapOverflow     bool   `json:"gap_overflow"`
	SourceConflicts uint16 `json:"source_conflicts"`
	// AwaitingBaseline counts streams still owing a restatement and
	// ConflictOverflow says local conflict evidence was evicted. Both are
	// counts of the host's own bookkeeping, not topology: they say a host is
	// not telling a complete story, which is the one thing about local
	// integrity the cloud is any use for.
	AwaitingBaseline uint16 `json:"awaiting_baseline"`
	ConflictOverflow bool   `json:"conflict_overflow"`

	ProposalClasses []ProjectedProposalClass `json:"proposal_classes,omitempty"`
}

func asConnectivityProjection(payload any) (ConnectivityProjection, bool) {
	switch value := payload.(type) {
	case ConnectivityProjection:
		return value, true
	case *ConnectivityProjection:
		if value == nil {
			return ConnectivityProjection{}, false
		}
		return *value, true
	default:
		return ConnectivityProjection{}, false
	}
}

// boundedToken accepts the alphabet an enumeration member uses. Anything that
// needs more than this is not an enumeration, and does not belong here.
func boundedToken(value string) bool {
	if value == "" || len(value) > 48 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return true
}

func validateConnectivityProjection(payload any) error {
	value, ok := asConnectivityProjection(payload)
	if !ok {
		return ErrPayloadType
	}
	if value.SnapshotGeneration == 0 || value.ReducerVersion == 0 {
		return ErrInvalidField
	}
	if !boundedToken(value.Aggregate) || !boundedToken(value.Authorization) ||
		!boundedToken(value.AuthorizationReason) {
		return ErrInvalidField
	}
	if len(value.Components) == 0 || len(value.Components) > MaxProjectedComponents {
		return ErrInvalidField
	}
	for _, component := range value.Components {
		if !boundedToken(component.Component) || !boundedToken(component.State) ||
			!boundedToken(component.Reason) || !component.Freshness.valid() {
			return ErrInvalidField
		}
	}
	if len(value.ProposalClasses) > MaxProjectedComponents {
		return ErrInvalidField
	}
	for _, class := range value.ProposalClasses {
		if !boundedToken(class.Class) || class.Count == 0 {
			return ErrInvalidField
		}
	}
	return nil
}
