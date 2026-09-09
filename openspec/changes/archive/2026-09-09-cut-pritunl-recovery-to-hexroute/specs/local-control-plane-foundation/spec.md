## MODIFIED Requirements

### Requirement: Disjoint observe-only runtime

Hexroute SHALL install and execute pre-cutover local components under paths,
labels, sockets, state and logs that are disjoint from the active Twilight
runtime, and observe-only execution SHALL NOT mutate production state.

Pritunl recovery is no longer pre-cutover. Under an active signed generation
granting that capability, the user runtime MAY submit Pritunl credentials and
the root runtime MAY restart the one named Pritunl service on a revalidated
credential-free request. Every other production mutation — routes, the tunnel,
and any other process — SHALL remain outside what a local component may do until
its own ownership cutover passes.

#### Scenario: Observe-only runtime evaluates host state

- **WHEN** `hexrouted` or `hexroute-userd` runs before active cutover
- **THEN** it may publish observations and proposed actions
- **AND** it does not apply routes or restart production processes other than the Pritunl service it has been authorized to restart

#### Scenario: Recovery is authorized

- **WHEN** an active generation grants the Pritunl recovery capability
- **THEN** the user runtime may submit Pritunl credentials and the root runtime may restart that one service
- **AND** no other capability is granted by the same generation being active

#### Scenario: A route or the tunnel is proposed

- **WHEN** a local component would apply a route or change the tunnel
- **THEN** it is refused, because that ownership has not been cut over
