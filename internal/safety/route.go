package safety

import (
	"errors"
	"fmt"
)

type LinkClass string

const (
	LinkPhysical    LinkClass = "physical"
	LinkUpstreamVPN LinkClass = "upstream_vpn"
	LinkTwilightTUN LinkClass = "twilight_tun"
)

type RouteRole string

const (
	RouteIngress RouteRole = "ingress"
	RouteScoped  RouteRole = "scoped"
)

type Route struct {
	Role RouteRole
	Link LinkClass
}

type RoutePlan struct {
	Routes []Route
}

var ErrIngressSelfRoute = errors.New("ingress route resolves through twilight tun")

func ValidateRoutePlan(plan RoutePlan) error {
	for index, route := range plan.Routes {
		if route.Role == RouteIngress && route.Link == LinkTwilightTUN {
			return fmt.Errorf("%w at route index %d", ErrIngressSelfRoute, index)
		}
	}
	return nil
}
