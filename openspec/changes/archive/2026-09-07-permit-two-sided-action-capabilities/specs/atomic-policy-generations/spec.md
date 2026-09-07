## MODIFIED Requirements

### Requirement: Selector ambiguity rejection

The compiler SHALL reject overlapping host, port, path, route/CIDR, action or
credential-reference selectors with different role, route, TLS, protocol,
signing or action semantics. Wildcard and concrete selectors SHALL NOT imply a
specificity override, while exact selectors with identical semantics SHALL be
deduplicated.

Overlap across the two domains SHALL be judged by what the overlap contradicts.
A credential reference, a route and an endpoint are each singular on the host,
so two domains claiming one SHALL be rejected as a cross-domain ownership
violation. An action capability is not singular: the compiled safety envelope
assigns capability and target per domain and MAY assign the same pair to both,
which the compiler SHALL have already verified before looking for conflicts. Two
domains authorized for the same capability and target SHALL therefore compile,
including with differing effects, because each domain's payload is evaluated on
its own and a partial rollback leaves exactly that state.

#### Scenario: Route selectors overlap with different paths

- **WHEN** two route or CIDR selectors overlap but assign different ownership or paths
- **THEN** the complete candidate is rejected before signing

#### Scenario: Wildcard overlaps a concrete conflicting selector

- **WHEN** wildcard and concrete selectors overlap with different semantics
- **THEN** the compiler reports a bounded conflict instead of selecting the more specific rule

#### Scenario: Duplicate selectors are semantically identical

- **WHEN** two exact selectors have identical authorization semantics
- **THEN** canonical compilation deduplicates them
- **AND** their source order does not affect the digest

#### Scenario: Both domains are authorized for one two-sided capability

- **WHEN** a candidate grants one capability and target to both domains and the envelope permits it in both
- **THEN** the candidate compiles
- **AND** each domain receives only its own rule

#### Scenario: One capability is revoked from a single domain

- **WHEN** a candidate grants a capability and target in one domain and denies it in the other
- **THEN** the candidate compiles, because each payload is evaluated on its own

#### Scenario: Two domains claim one credential

- **WHEN** two domains name the same credential reference
- **THEN** the candidate is rejected, because a credential has one owner

### Requirement: Closed policy source composition

The policy compiler SHALL compose a non-overridable compiled safety baseline,
disjoint root and user policy namespaces, and bounded operator authorization
leases into one complete effective snapshot. Operator leases SHALL only narrow
predeclared capabilities by intersection, compiled denies SHALL win, and no
source SHALL use last-writer-wins behavior.

A capability the envelope permits in both domains SHALL be compilable in both.
The envelope declares what each domain may be authorized for, and a compiler
that refuses what the envelope permits leaves an authority that can be written
down, reviewed and signed but never issued.

#### Scenario: Operator source attempts to expand the safety envelope

- **WHEN** an operator source authorizes a capability or selector outside the compiled safety baseline
- **THEN** the compiler rejects the complete candidate
- **AND** no domain payload is eligible for prepare or activation

#### Scenario: Root and user sources declare disjoint valid policy

- **WHEN** root and user sources stay inside their owned namespaces and the compiled envelope
- **THEN** the compiler includes both in one effective snapshot
- **AND** neither daemon receives policy owned by the other privilege domain

#### Scenario: Compiled deny intersects an operator lease

- **WHEN** an operator lease intersects both an allowed selector and a compiled deny
- **THEN** the denied portion remains unauthorized
- **AND** the lease cannot broaden the effective selector set

#### Scenario: The envelope permits a capability in both domains

- **WHEN** the compiled envelope lists a capability under both domains
- **THEN** a candidate granting it in both compiles, for every such capability rather than for a named one

#### Scenario: A compiled payload authorizes the act it was compiled for

- **WHEN** a candidate granting a capability is compiled and its payload evaluated under an active generation and lease
- **THEN** the act is authorized in each domain the candidate granted it to
