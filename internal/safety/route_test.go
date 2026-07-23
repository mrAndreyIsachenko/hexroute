package safety

import (
	"errors"
	"testing"
)

func TestValidateRoutePlanRejectsIngressOnTwilightTUN(t *testing.T) {
	tests := []struct {
		name string
		plan RoutePlan
	}{
		{
			name: "single ingress",
			plan: RoutePlan{Routes: []Route{
				{Role: RouteIngress, Link: LinkTwilightTUN},
			}},
		},
		{
			name: "one bad ingress rejects complete mixed plan",
			plan: RoutePlan{Routes: []Route{
				{Role: RouteIngress, Link: LinkPhysical},
				{Role: RouteScoped, Link: LinkTwilightTUN},
				{Role: RouteIngress, Link: LinkTwilightTUN},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateRoutePlan(test.plan)
			if !errors.Is(err, ErrIngressSelfRoute) {
				t.Fatalf("ValidateRoutePlan() error = %v, want %v", err, ErrIngressSelfRoute)
			}
		})
	}
}

func TestValidateRoutePlanAllowsScopedRouteOnTwilightTUN(t *testing.T) {
	plan := RoutePlan{Routes: []Route{
		{Role: RouteIngress, Link: LinkPhysical},
		{Role: RouteIngress, Link: LinkUpstreamVPN},
		{Role: RouteScoped, Link: LinkTwilightTUN},
	}}

	if err := ValidateRoutePlan(plan); err != nil {
		t.Fatalf("ValidateRoutePlan() unexpected error: %v", err)
	}
}
