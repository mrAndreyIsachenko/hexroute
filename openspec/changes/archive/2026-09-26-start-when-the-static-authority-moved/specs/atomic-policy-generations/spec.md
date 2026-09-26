# Atomic Policy Generations Specification Delta

## ADDED Requirements

### Requirement: A daemon outlives a change of static authority

A daemon whose stored active generation was compiled against a different static
authority SHALL start, report `restart_required` with reason `static_mismatch`,
and perform no mutation. It SHALL name the generation it cannot run — the bundle
and domain generations and the manifest digest — taken from the store's lineage,
which is the reading that deliberately does not compare a predecessor's static
digest.

It SHALL NOT refuse to run. Changing static authority is a configuration
installation and a guarded restart, after which the successor is installed and
activated, and activation goes through the daemons: a daemon that refuses to
start makes the only act that resolves the mismatch impossible. Measured
2026-09-25, installing a rebuilt safety envelope stopped both daemons on this
machine, each restarting every ten to twenty seconds under launchd, until the
configuration was rolled back.

Such a daemon SHALL carry the lineage's generation as the generation it holds,
so that the successor — which names its parent — can be prepared and activated
through it. A runtime carrying no generation refuses every candidate as a
downgrade, which would leave the mismatch reported and unresolvable. Measured
2026-09-26 on this machine, both daemons came up and named the mismatch, and
both refused the one bundle that ends it. Adopting the chain SHALL grant
nothing: no generation is active, the mutation gate refuses on that alone, and
lineage carries no payload, manifest or approval. A chain that would not survive
its own validation SHALL be reported and not adopted.

Nothing else SHALL change about what may be activated. A candidate whose static
digest differs from the installed one is still refused, and is still resolved by
installing the configuration it needs.

The other startup failures SHALL keep their answers. A corrupt store, an invalid
signature, a clock anomaly and a store that cannot be read are different
questions, and one of them becoming legible must not make the rest so.

#### Scenario: The installed static authority moved ahead of the generation in force

- **WHEN** a daemon starts and the generation its store holds active was compiled against a different static digest
- **THEN** it runs, reports `restart_required` with reason `static_mismatch`, and names that generation
- **AND** it authorizes no mutation

#### Scenario: The successor is installed and activated

- **WHEN** a generation compiled against the installed static authority, naming the held generation as its parent, is prepared and activated on such a daemon
- **THEN** it is accepted rather than refused as a downgrade, and the daemon reports it active in the ordinary way

#### Scenario: The chain it holds grants nothing

- **WHEN** a daemon holds a generation it cannot run and is asked to act
- **THEN** it refuses, because no generation is active

#### Scenario: The static authority matches

- **WHEN** a daemon starts and its store's active generation carries the installed static digest
- **THEN** nothing about its startup differs from before

#### Scenario: A store that cannot be read

- **WHEN** a daemon starts and its store is corrupt, unreadable, or carries an invalid signature
- **THEN** the answer it gives is the one it gave before this requirement, and is not `static_mismatch`
