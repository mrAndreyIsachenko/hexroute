# Incident Lifecycle

The maintenance worker reconciles validated condition signals into PostgreSQL
incidents. A fixed correlation key identifies one logical condition. A
transaction-scoped advisory lock and the active-correlation unique index
prevent concurrent workers from opening duplicate active incidents.

The first detection creates generation 1, a `new -> open` transition and any
trigger or supporting evidence links. Repeated observations update
`last_observed_at` without incrementing the generation. A severity or
actionability change creates a new generation; an escalation reopens an
acknowledged incident so alert delivery can evaluate the new state.

Acknowledgement creates an `open -> acknowledged` transition. It does not
resolve the condition or delete pending external delivery. A later validated
clear signal creates an `open|acknowledged -> resolved` transition and links
recovery or exclusion evidence. Silent-node decisions use:

- the latest accepted node event as trigger or recovery evidence;
- the signed sleep-start event as exclusion evidence;
- an explicit no-event result for a node that has never contacted the service.

Reconciliation is safe for at-least-once delivery. Duplicate evidence retains
its original role, duplicate signals do not manufacture transitions and a
stale detection cannot reopen a more recently resolved incident. A genuinely
new detection after resolution receives a new incident UUID and starts again
at generation 1.

Incident correlation is cloud telemetry only. It cannot mutate a node, restart
any service or alter routes.
