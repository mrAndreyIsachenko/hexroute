## ADDED Requirements

### Requirement: The repository carries the shape of every configuration a runtime reads

For each runtime it installs, the public repository SHALL carry an example
configuration holding every setting that runtime's decoder accepts, so the
configuration can be reconstructed, reviewed and compared against without
reading the one host that runs it.

A setting a decoder accepts and its example omits SHALL fail a gate, and so
SHALL a setting an example carries and the decoder no longer accepts. The gate
SHALL take the settings from the decoder itself rather than from a list written
beside it, because a list is the thing that was not updated: measured
2026-10-08, the root example carried 46 settings against the 112 the installed
configuration held, with no `tunnel_supervision` and no `policy_control` at all,
so ten settings — the whole tunnel executor — existed only on one machine.

The example SHALL be accepted by the same decoder the runtime uses, so that the
record is a template and not an inventory. It SHALL hold placeholder values and
no live ones; where a setting is key material, the placeholder SHALL be
self-consistent rather than live — a zero key with its own true digest satisfies
a decoder that checks one against the other, and reveals nothing.

This is the boundary stated from the side that was missing. The requirement that
the public repository hold no live deployment state says what may not be there;
this says what must: the shape, never the values.

#### Scenario: A setting is added to a runtime

- **WHEN** a decoder gains a setting and the example does not
- **THEN** the gate refuses and names the setting, before any install is attempted

#### Scenario: A setting is removed from a runtime

- **WHEN** a decoder no longer accepts a setting the example still carries
- **THEN** the gate refuses, so the record cannot describe a configuration the runtime would reject

#### Scenario: The example is read by the decoder

- **WHEN** the runtime's own decoder is given the example
- **THEN** it accepts it, because the record is the same kind of thing as the file it stands for

#### Scenario: A setting is key material

- **WHEN** the example must carry a key, a fingerprint or a digest
- **THEN** it carries a placeholder consistent with itself and with no live material, so the decoder's own cross-check passes and nothing is revealed

#### Scenario: The comparison before an install has a record

- **WHEN** an operator compares a prepared configuration against the installed one
- **THEN** the repository holds the shape both are instances of, so a missing setting is visible in the repository rather than only on the host
