## ADDED Requirements

### Requirement: Synthetic pre-cutover reconciliation boundary

The pre-cutover local control plane MAY host the generation-bound reconciliation
engine only after its atomic-policy and observable-connectivity prerequisites
are qualified. Until a separate capability and ownership cutover is approved,
the compiled registry SHALL contain only synthetic adapters and SHALL expose no
production network, process, Pritunl or credential mutation path.

#### Scenario: Reconciler starts before production cutover

- **WHEN** qualified root or user Hexroute runtime enables the reconciliation engine
- **THEN** it can evaluate and replay only synthetic capability actions
- **AND** Twilight remains the sole production owner

#### Scenario: Production capability identifier is requested

- **WHEN** any local caller names a route, DNS, firewall, process, tunnel, Pritunl, Keychain or credential capability absent from the static synthetic registry
- **THEN** typed IPC and translation reject the request
- **AND** no lease, attempt or side effect is created

### Requirement: Domain-owned reconciliation state

Root and user daemons SHALL persist and execute only their own domain's
reconciliation state. Aggregate observation SHALL NOT grant root authority over
user actions or credentials, and user state SHALL NOT authorize root network or
process operations.

#### Scenario: Root receives a user-domain proposal

- **WHEN** a valid proposal targets a user-owned component or capability
- **THEN** root rejects it as an ownership conflict
- **AND** it does not forward, translate or execute the proposal on the user's behalf

#### Scenario: One domain is unavailable

- **WHEN** root or user runtime cannot provide a matching qualified generation and receipt
- **THEN** cross-domain reconciliation remains unavailable
- **AND** the available domain continues observe-only status without impersonating the missing owner
