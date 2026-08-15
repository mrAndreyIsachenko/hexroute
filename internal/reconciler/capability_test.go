package reconciler

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultRegistryContainsOnlySyntheticCapabilities(t *testing.T) {
	registry := DefaultSyntheticRegistry()
	if !registry.SyntheticOnly() {
		t.Fatal("default registry is not synthetic-only")
	}
	ids := registry.IDs()
	if len(ids) != 3 {
		t.Fatalf("default registry ids = %v", ids)
	}
	for _, id := range ids {
		if !strings.HasPrefix(string(id), "synthetic.reconciler.") {
			t.Fatalf("non-synthetic capability id %q", id)
		}
		if _, ok := registry.Lookup(id); !ok {
			t.Fatalf("Lookup(%q) failed", id)
		}
	}
}

func TestRegistryRejectsProductionCapabilityIdentifiers(t *testing.T) {
	for _, id := range []CapabilityID{
		"route.scoped",
		"dns.resolver",
		"firewall.rule",
		"process.restart",
		"tunnel.rebuild",
		"pritunl.reconnect",
		"keychain.credential",
		"synthetic.reconciler.route",
		"synthetic.reconciler.dns",
		"synthetic.reconciler.credential",
	} {
		t.Run(string(id), func(t *testing.T) {
			_, err := NewRegistry([]CapabilityDescriptor{{
				ID:             id,
				AdapterID:      "synthetic.adapter.noop",
				OperationClass: OperationSyntheticNoop,
			}})
			if !errors.Is(err, ErrInvalidCapabilityRegistry) {
				t.Fatalf("NewRegistry(%q) error = %v, want %v", id, err, ErrInvalidCapabilityRegistry)
			}
		})
	}
}

func TestRegistryRejectsProductionAdapterIdentifiersAndDuplicates(t *testing.T) {
	tests := [][]CapabilityDescriptor{
		{
			{ID: CapabilitySyntheticNoop, AdapterID: "route.adapter", OperationClass: OperationSyntheticNoop},
		},
		{
			{ID: CapabilitySyntheticNoop, AdapterID: "synthetic.adapter.keychain", OperationClass: OperationSyntheticNoop},
		},
		{
			{ID: CapabilitySyntheticNoop, AdapterID: "synthetic.adapter.noop", OperationClass: "shell"},
		},
		{
			{ID: CapabilitySyntheticNoop, AdapterID: "synthetic.adapter.noop", OperationClass: OperationSyntheticNoop},
			{ID: CapabilitySyntheticNoop, AdapterID: "synthetic.adapter.memory", OperationClass: OperationSyntheticState},
		},
	}
	for index, test := range tests {
		if _, err := NewRegistry(test); !errors.Is(err, ErrInvalidCapabilityRegistry) {
			t.Fatalf("case %d NewRegistry() error = %v, want %v", index, err, ErrInvalidCapabilityRegistry)
		}
	}
}
