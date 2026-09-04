# Ingress Fleet Purpose Specification

## Purpose

Record what each ingress host is for, so that a host's provider, region and
measured latency are read against its purpose rather than against latency alone.
Count redundancy in independent failure domains rather than in configured
entries, and require that any comparison between providers rest on measurements
taken over comparable paths.

## Requirements

### Requirement: Every ingress host has a recorded purpose

Each host in the ingress fleet SHALL have its purpose recorded before its
placement can be judged. A host's provider, region and measured latency SHALL be
readable against that purpose, so that a host which is slow on purpose is not
read as a host which is slow by mistake.

The recorded purposes SHALL distinguish at least: serving the operator's primary
network, presenting an address in a named country, and providing an independent
failure domain. A host MAY serve more than one purpose, and a purpose MAY be
served by more than one host.

Where a jurisdiction filters the operator's access, an ingress placed inside
that jurisdiction SHALL NOT be recorded as serving access from it.

#### Scenario: A host is slower than its peers

- **WHEN** a host's measured latency is worse than every other host in the fleet
- **THEN** its recorded purpose is read before its placement is called a defect
- **AND** a host whose purpose is an address in a named country is not relocated for latency alone

#### Scenario: A purpose has no host

- **WHEN** a recorded purpose is served by no host in the fleet
- **THEN** the gap is recorded as owed work rather than left to be inferred from placement

#### Scenario: An ingress is proposed inside a filtered jurisdiction

- **WHEN** a host is proposed in a jurisdiction that filters the operator's access
- **THEN** it is not recorded as serving access from that jurisdiction
- **AND** the reason is recorded with it

### Requirement: Fleet redundancy is counted in hosts, not names

The fleet's redundancy SHALL be stated in independent failure domains, not in
configured entries. Two entries that resolve to one machine SHALL be recorded as
one failure domain, whatever they are named and however the client selects
between them.

#### Scenario: Two entries share a machine

- **WHEN** two configured ingress entries reach the same host
- **THEN** the fleet's recorded redundancy counts them once
- **AND** the shared machine is named so that a client's selection cannot present it as two paths

#### Scenario: Redundancy is reported

- **WHEN** the fleet's redundancy is documented
- **THEN** the count of independent hosts is stated rather than the count of entries

### Requirement: Provider comparison requires comparable measurement

A claim that one provider is more available than another SHALL rest on
measurements taken over comparable paths. Where hosts are probed over different
routes, the measurement SHALL NOT be used to compare providers, and the reason
SHALL be recorded with it.

Absence of comparable measurement SHALL be recorded as absence, not resolved by
using the measurement that exists.

#### Scenario: Targets are probed over different routes

- **WHEN** one target is probed through a tunnel and another directly
- **THEN** the result is not admissible as a comparison between their providers
- **AND** the route of each measurement is recorded alongside its result

#### Scenario: A provider is selected without comparable evidence

- **WHEN** no comparable measurement exists for a placement decision
- **THEN** the decision is recorded as a judgement with its reasons
- **AND** it is not documented as evidence-based

### Requirement: Documented lifecycle state distinguishes record from world

Provider lifecycle documentation SHALL distinguish what the public record proves
from what is deployed. Where a workload is instantiated, provisioned or carrying
traffic and the public repository proves none of it, the documentation SHALL say
both, and SHALL NOT report the earlier state as the current one.

Live addresses, provider identities and deployment evidence SHALL remain outside
this repository; the distinction is stated without publishing what proves it.

#### Scenario: A workload is deployed and the public record is silent

- **WHEN** a private workload is instantiated and no public repository fact proves it
- **THEN** the documentation states the deployed state and states that the public record does not prove it
- **AND** no live address, provider identity or evidence artifact is published to make the point

#### Scenario: A reader asks what state the provider is in

- **WHEN** the lifecycle table is read
- **THEN** the current state is the deployed state, not the last state the public record can prove
