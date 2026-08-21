# Operational Acceptance Drill Specification

## Purpose

Define the non-mutating, user-visible acceptance gate that proves the operator
can still work before Hexroute proceeds with future runtime ownership, cutover
or failover-enabling changes.

## Requirements

### Requirement: User-visible operational acceptance gate

Hexroute SHALL provide an operational acceptance drill that verifies
user-visible work paths before future runtime cutover, production adapter
activation, ownership transfer or failover enablement. The gate SHALL cover
Codex/ChatGPT reachability, ordinary Internet access, GitLab web access, Git
transport, Pritunl process visibility plus read-only profile connectivity,
AdGuard coexistence, Telegram/monitoring visibility and fallback path
availability.
When a Twilight directory is configured, the gate SHALL also verify Twilight
self-containment with the read-only carrier status check so an out-of-band VPN
carrier cannot be counted as Hexroute/Twilight self-recovery.

#### Scenario: Core work paths pass

- **WHEN** the operator runs the operational acceptance drill for a candidate runtime state
- **THEN** every required core work path is recorded as passing with bounded redacted evidence
- **AND** the candidate may proceed to its next non-production qualification step

#### Scenario: A core work path fails

- **WHEN** Codex/ChatGPT, ordinary Internet, GitLab, Git transport, Pritunl, AdGuard coexistence or fallback visibility fails the drill
- **THEN** the next runtime cutover or mutation-enabling step is blocked
- **AND** the failure remains visible until fixed or explicitly waived with owner, reason and expiry

### Requirement: Non-mutating smoke probes

The drill SHALL include a read-only smoke script that collects bounded status
for configured target labels without stopping, starting, restarting,
reconfiguring or assuming ownership of Twilight, AdGuard, Pritunl, sing-box,
routes, DNS, launchd services, provider resources or cloud services.

#### Scenario: Smoke script runs

- **WHEN** the smoke script is executed with its default mode
- **THEN** it performs only read-only probes and status collection
- **AND** it does not invoke service-control, route-mutation, DNS-mutation, provider-mutation or credential-access commands

#### Scenario: Required target is absent

- **WHEN** a private target label is not configured
- **THEN** the script reports the related check as `not_configured`
- **AND** it does not substitute a guessed endpoint or inspect private configuration outside the allowed input file

#### Scenario: HTTP target returns an authentication or challenge response

- **WHEN** an HTTP smoke target returns a `2xx`, `3xx` or `4xx` response
- **THEN** the script records the target as reachable
- **AND** browser login or message-send usability remains a separate manual checkpoint

#### Scenario: Fallback HTTP target uses an explicit local proxy

- **WHEN** a smoke target is configured with an explicit per-check proxy
- **THEN** the script probes that target through the configured proxy
- **AND** records only the bounded pass/fail class, not the proxy URL or target
  URL
- **AND** checks without an explicit proxy continue to use the normal direct
  path

#### Scenario: Pritunl process is running but the profile is not connected

- **WHEN** the Pritunl service process exists but the configured profile is not `Active` with a client address
- **THEN** the process probe MAY pass
- **BUT** the Pritunl profile probe fails with a bounded result class
- **AND** the drill cannot pass on process visibility alone

### Requirement: Manual checkpoints are explicit evidence

The drill SHALL distinguish automated read-only probes from manual checkpoints
for browser session behavior, ChatGPT/Codex message send, Pritunl OTP fallback,
sleep/wake, reboot and network-loss recovery. Manual checkpoints SHALL require
explicit operator confirmation or be recorded as incomplete.

#### Scenario: Manual checkpoint is not confirmed

- **WHEN** a browser, OTP, sleep/wake, reboot or network-loss checkpoint lacks explicit operator confirmation
- **THEN** the drill records that checkpoint as incomplete
- **AND** it cannot be counted as a passing acceptance result

#### Scenario: Post-sleep checkpoint runs

- **WHEN** the operator resumes the machine after sleep and reruns the drill
- **THEN** the evidence identifies the phase as post-sleep
- **AND** Codex/ChatGPT, GitLab/Git, Pritunl and fallback checks are evaluated again rather than inherited from the baseline phase

#### Scenario: Activation or recovery needed external manual rescue

- **WHEN** a post-activation, post-sleep, post-reboot or post-network-loss phase required the operator to enable an out-of-band rescue path before the drill could pass
- **THEN** the drill records missing or failed `no_external_rescue_used` evidence
- **AND** the phase cannot be counted as a self-recovery pass

#### Scenario: Twilight carrier still depends on an external VPN

- **WHEN** a Twilight directory is configured
- **AND** Twilight reports that no active ingress is self-contained on the physical carrier
- **THEN** the drill records `twilight_carrier` as failed with an external-rescue result class
- **AND** the phase cannot be counted as a self-recovery pass even if manual checkpoints are set to pass

### Requirement: Redacted evidence bundle

The drill SHALL emit a local evidence bundle containing only redacted target
labels, pass/fail classes, timing buckets, exit classes, phase identity,
timestamp, tool versions and bounded diagnostic codes. The bundle SHALL NOT
contain raw private hostnames, IP addresses, URLs with tokens, Git remotes,
session identifiers, cookies, OTP/PIN values, credentials, command stdout that
may include secrets or raw logs.

#### Scenario: Evidence is written

- **WHEN** a drill run completes
- **THEN** a redacted evidence bundle is written outside tracked source by default
- **AND** the public repository contains only schemas, scripts, docs and tests for the evidence shape

#### Scenario: Protected value reaches evidence candidate

- **WHEN** a known protected value canary appears in an evidence candidate
- **THEN** evidence serialization or its test gate fails
- **AND** the value is not persisted or printed by the drill

### Requirement: Phase-specific recovery coverage

The drill SHALL define baseline, post-Twilight/Hexroute activation,
post-sleep, post-reboot and post-network-loss phases. A phase SHALL pass only
when its own fresh probes and manual checkpoints pass; passing evidence from a
prior phase SHALL NOT be reused as proof for a later phase.

#### Scenario: Reboot phase is evaluated

- **WHEN** the operator runs the drill after a reboot
- **THEN** the reboot phase records fresh work-path evidence
- **AND** prior baseline evidence is linked only as context, not as a pass

#### Scenario: Network-loss recovery phase is evaluated

- **WHEN** the operator runs the drill after losing and restoring network access
- **THEN** the network-loss phase records whether fallback access and primary paths recovered
- **AND** the result does not trigger any automatic repair action

### Requirement: Acceptance result is independent from production authority

Passing the operational acceptance drill SHALL NOT grant production mutation
authority, enable a production adapter, cut ownership from Twilight, change
policy generation, alter cloud authority or waive future safety requirements.

#### Scenario: Drill passes before a cutover proposal

- **WHEN** every acceptance phase required by a later cutover change has passed
- **THEN** that later change may cite the acceptance evidence as a prerequisite
- **AND** it still requires its own OpenSpec change, safety envelope, rollback and approval
