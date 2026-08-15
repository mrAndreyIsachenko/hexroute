package reconciler

import (
	"errors"
	"regexp"
	"sort"
	"strings"
)

type CapabilityID string

const (
	CapabilitySyntheticNoop         CapabilityID = "synthetic.reconciler.noop"
	CapabilitySyntheticMemory       CapabilityID = "synthetic.reconciler.memory"
	CapabilitySyntheticCrashFixture CapabilityID = "synthetic.reconciler.crash_fixture"
)

type OperationClass string

const (
	OperationSyntheticNoop         OperationClass = "synthetic_noop"
	OperationSyntheticState        OperationClass = "synthetic_state"
	OperationSyntheticCrashFixture OperationClass = "synthetic_crash_fixture"
)

type CapabilityDescriptor struct {
	ID             CapabilityID   `json:"id"`
	AdapterID      string         `json:"adapter_id"`
	OperationClass OperationClass `json:"operation_class"`
}

type Registry struct {
	descriptors map[CapabilityID]CapabilityDescriptor
}

var (
	ErrInvalidCapabilityRegistry = errors.New("invalid reconciliation capability registry")
	capabilityIDPattern          = regexp.MustCompile(`^synthetic\.reconciler\.[a-z][a-z0-9_]{0,63}$`)
	adapterIDPattern             = regexp.MustCompile(`^synthetic\.adapter\.[a-z][a-z0-9_]{0,63}$`)
)

func DefaultSyntheticRegistry() Registry {
	registry, err := NewRegistry([]CapabilityDescriptor{
		{
			ID:             CapabilitySyntheticNoop,
			AdapterID:      "synthetic.adapter.noop",
			OperationClass: OperationSyntheticNoop,
		},
		{
			ID:             CapabilitySyntheticMemory,
			AdapterID:      "synthetic.adapter.memory",
			OperationClass: OperationSyntheticState,
		},
		{
			ID:             CapabilitySyntheticCrashFixture,
			AdapterID:      "synthetic.adapter.crash_fixture",
			OperationClass: OperationSyntheticCrashFixture,
		},
	})
	if err != nil {
		panic(err)
	}
	return registry
}

func NewRegistry(descriptors []CapabilityDescriptor) (Registry, error) {
	if len(descriptors) == 0 || len(descriptors) > 16 {
		return Registry{}, ErrInvalidCapabilityRegistry
	}
	owned := make(map[CapabilityID]CapabilityDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.validate() != nil {
			return Registry{}, ErrInvalidCapabilityRegistry
		}
		if _, exists := owned[descriptor.ID]; exists {
			return Registry{}, ErrInvalidCapabilityRegistry
		}
		owned[descriptor.ID] = descriptor
	}
	return Registry{descriptors: owned}, nil
}

func (registry Registry) Lookup(id CapabilityID) (CapabilityDescriptor, bool) {
	descriptor, ok := registry.descriptors[id]
	return descriptor, ok
}

func (registry Registry) IDs() []CapabilityID {
	ids := make([]CapabilityID, 0, len(registry.descriptors))
	for id := range registry.descriptors {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	return ids
}

func (registry Registry) SyntheticOnly() bool {
	if len(registry.descriptors) == 0 {
		return false
	}
	for id, descriptor := range registry.descriptors {
		if id != descriptor.ID || descriptor.validate() != nil {
			return false
		}
	}
	return true
}

func (descriptor CapabilityDescriptor) validate() error {
	if !capabilityIDPattern.MatchString(string(descriptor.ID)) ||
		!adapterIDPattern.MatchString(descriptor.AdapterID) ||
		!descriptor.OperationClass.valid() ||
		hasProductionFragment(string(descriptor.ID)) ||
		hasProductionFragment(descriptor.AdapterID) {
		return ErrInvalidCapabilityRegistry
	}
	return nil
}

func (class OperationClass) valid() bool {
	return class == OperationSyntheticNoop ||
		class == OperationSyntheticState ||
		class == OperationSyntheticCrashFixture
}

func hasProductionFragment(value string) bool {
	normalized := strings.ToLower(value)
	for _, fragment := range []string{
		"adguard", "credential", "dns", "firewall", "keychain", "mtg", "pritunl",
		"reality", "route", "sing-box", "transport", "tun", "tunnel", "twilight",
		"vless", "vpn", "xray",
	} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
