## ADDED Requirements

### Requirement: A host reports its own events

A host whose events can matter to someone away from it SHALL upload them, and
the upload SHALL NOT run inside the loop that observes and acts. The runtime
that owns the tunnel is the runtime whose cycle must keep its period; a network
call in that cycle is a tunnel nobody is supervising for as long as the call
hangs. The uploader SHALL therefore run as its own scheduled privileged job,
reading the upload spool and never the local event archive — which the
acknowledgement-driven gap repair requirement already states, for the reason it
already gives.

That job SHALL run on two triggers and SHALL say which one woke it. One is an
interval, for the ordinary stream nobody is waiting for. The other is the
runtime recording an incident: the job SHALL start then, because an alert is the
one record whose value is a function of how old it is. The record that a record
is waiting SHALL be removed only after the upload is acknowledged, so a job that
dies mid-upload is started again rather than forgotten, and the interval SHALL
collect what a missed trigger left.

The node key SHALL be generated where the operator is present to register its
public half, which is at installation, and an existing key SHALL be kept rather
than replaced. A key created inside a scheduled job is a key nobody registered,
and its first uploads fail authentication as silence.

A reporting host SHALL be registered with an expected heartbeat it can meet
while behaving normally. A host that sleeps nightly and is registered to report
every minute is a host that alerts every night; measured 2026-10-09, a silent
node requires action and its delivery therefore ignores the night window
entirely. Until a host announces its own sleep, its expectation SHALL be long
enough that ordinary sleep does not reach it.

Silence SHALL remain evaluated for a reporting host, because an uploader that
has stopped is the one failure a pushed alert cannot report: nothing arrives,
and nothing says that nothing arrived.

#### Scenario: An incident is recorded while the interval is far away

- **WHEN** the runtime records an incident between two scheduled runs
- **THEN** the upload job starts without waiting for the interval
- **AND** the record saying an upload is due survives until the batch is acknowledged

#### Scenario: The upload job dies before it is acknowledged

- **WHEN** the job stops after sending and before applying the acknowledgement
- **THEN** the next run uploads the same records again, idempotently
- **AND** the interval starts that run even if nothing new was recorded

#### Scenario: The observing cycle is not delayed by an upload

- **WHEN** the registry or the network does not answer
- **THEN** the cycle that observes and acts keeps its period
- **AND** the failure is the upload job's, which reports what refused it

#### Scenario: A key is already present at installation

- **WHEN** the installer runs on a host that already has a node key
- **THEN** the existing key is kept and no new identity is created

#### Scenario: A host that sleeps is not reported silent for sleeping

- **WHEN** a host that has not announced its sleep is suspended overnight
- **THEN** its expected heartbeat is long enough that the gap does not reach its deadline

#### Scenario: The uploader stops being heard from

- **WHEN** a registered reporting host uploads nothing for longer than its deadline
- **THEN** silence is evaluated and an incident is opened for it

## MODIFIED Requirements

### Requirement: Durable incident and retention model

The worker SHALL correlate sleep-aware heartbeat state, open and resolve durable
incidents, maintain configured retention and calculate eligible-interval SLOs.

An incident a host reported SHALL become a signal in the same way an inferred one
does, and SHALL be correlated by the node, the category and the component rather
than by the identity the host gave it. A host names an occurrence; the cloud
holds a condition. Measured 2026-10-09, the spool records an overflow incident on
every append that meets its size bound and numbers each one by its own sequence,
so correlating on what the host named would open a new incident every cycle, each
one requiring action and each one therefore ignoring the night window. Which
occurrence opened a condition SHALL be kept as evidence, where it is a fact about
the condition rather than its name.

A host that says a condition ended SHALL clear it. A condition that can only ever
open teaches an operator that the channel it arrives on is not worth reading,
because the next alert arrives against a background of one that never closed.

#### Scenario: A sleeping node is silent

- **WHEN** a signed sleep interval covers the missing heartbeat window
- **THEN** the worker excludes that interval from false downtime and SLO penalties

#### Scenario: Detailed telemetry expires

- **WHEN** a record crosses its configured retention boundary
- **THEN** bounded detail is removed while required incident, deployment and aggregate history remains

#### Scenario: A host reports an incident

- **WHEN** an uploaded event says a condition was detected on that node
- **THEN** a signal opens or updates one incident correlated by node, category and component
- **AND** the occurrence the host named is kept as that incident's evidence

#### Scenario: The same condition recurs while it is already open

- **WHEN** a host reports the same category and component again with nothing else changed
- **THEN** the open incident is updated rather than a second one opened
- **AND** no further delivery is planned for a transition that did not happen

#### Scenario: A host reports that a condition ended

- **WHEN** an uploaded event resolves a condition that is open for that node
- **THEN** the incident is cleared

### Requirement: Actionable bounded alert delivery

The worker SHALL deliver deduplicated incident transitions through Telegram,
track transactional delivery state and support night suppression plus a morning
recovery digest. During write freeze, it SHALL remain alive without claiming,
delivering, or persisting alert work.

A delivery SHALL NOT be planned on a channel nothing can claim. Measured
2026-10-09, every actionable incident planned a `local_macos` delivery that no
worker claims — the store refuses that channel — and that retention cannot
remove, because it removes only deliveries that reached a terminal state. A row
that can be neither served nor expired is not a pending delivery; it is a note
about an intention, kept in the one place that charges for it. A channel SHALL
be planned in the same change that gives it a claimer, so that the plan and the
claim cannot be out of step.

#### Scenario: The same incident transition is reconciled twice

- **WHEN** worker execution retries after an ambiguous delivery result
- **THEN** the persisted delivery identity prevents an unintended duplicate alert

#### Scenario: Alert cycle occurs during freeze

- **WHEN** the worker reaches an alert cycle while write freeze is active
- **THEN** it performs no Telegram delivery and no database mutation

#### Scenario: A channel has no claimer

- **WHEN** planning a delivery for a channel no worker can claim
- **THEN** no row is written for it
- **AND** a gate refuses a planned channel that nothing claims

### Requirement: Bounded acknowledgement-driven sequence gap repair

Signed ingestion acknowledgements SHALL bind node identity, request identity and
the durably accepted high-watermark and MAY include a bounded sorted set of
missing node-sequence ranges. The local uploader SHALL replay only exact retained
immutable records for those ranges from the upload journal, SHALL NOT read the
local event archive as a source for replay, and SHALL NOT synthesize records,
renumber events or expose the acknowledgement to policy, reduction or action
packages.

That last prohibition SHALL be held by what the reply path can reach and not only
by what it does. A test of behaviour holds the handler that exists; a reply path
that cannot reach a mutation package holds the one written next. Removing
acknowledged records from the upload spool SHALL remain the only local change an
acknowledgement causes, and it costs nothing that is not kept, because the
archive holds the same records on its own bounds.

#### Scenario: Server reports a retained missing range

- **WHEN** an authenticated acknowledgement names a bounded missing sequence range still present in the local journal
- **THEN** the uploader resends the exact signed records idempotently
- **AND** local connectivity and action state remain unchanged

#### Scenario: Gap request exceeds its bounds

- **WHEN** an acknowledgement contains too many ranges, an oversized range, an invalid order, another node identity or an unrelated request binding
- **THEN** the uploader rejects the gap request
- **AND** it performs no unbounded scan or local control operation

#### Scenario: Requested evidence has expired locally

- **WHEN** retention no longer contains all records in a valid requested range
- **THEN** the uploader emits one bounded redacted `telemetry_gap_unrecoverable` record after the gap
- **AND** it leaves the server-side gap visible while allowing newer telemetry uploads

#### Scenario: The archive still holds a record the journal has dropped

- **WHEN** a valid requested range is absent from the upload journal but present in the local archive
- **THEN** the uploader still reports the gap as unrecoverable
- **AND** it does not upload the archived copy, because archiving a record is not a decision to send it

#### Scenario: A reply path is given a way to mutate

- **WHEN** the package handling ingestion acknowledgements can reach a package that holds a lease, a plan, a command or an executor
- **THEN** a gate refuses, naming the package and the path
