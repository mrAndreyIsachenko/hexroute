# Eligible-Interval SLOs

Hexroute calculates immutable hourly or UTC-day aggregates. A later worker pass
upserts the same `(granularity, target, service, objective, window_start)` row,
so delayed recovery evidence can finalize an earlier window without creating a
duplicate. Every calculation is bounded to 4096 state changes or recovery
opportunities.

Availability objectives use piecewise-constant state:

- Twilight is eligible only while the Mac is awake and at least one physical
  carrier is available. Eligible time is good while the Twilight transport is
  ready and bad otherwise.
- Telegram is eligible while at least one configured provider is reachable.
  Eligible time is good while at least one MTProto proxy is healthy. One failed
  proxy with another healthy proxy remains good.

Sleep, missing carrier, no reachable Telegram provider and an explicitly lost
prerequisite path are excluded rather than recorded as failures. The state at
the beginning of an availability window is mandatory; the calculator refuses
to guess unknown leading time.

Recovery objectives count bounded opportunities:

- Codex fallback: usable within 30 seconds after confirmed normal-path failure
  while Twilight SOCKS/DoH remains ready;
- Pritunl: connected within 3 minutes and within 10 minutes after the outer
  Twilight path becomes ready;
- Telegram client failover: healthy through the alternate reachable provider
  within 60 seconds.

An unresolved opportunity does not fail until its deadline has elapsed. If its
prerequisite path disappears before recovery and before the objective
deadline, the observed attempt duration is excluded. This prevents an outer
transport or provider outage from being attributed to the dependent service.
Pritunl's 3-minute and 10-minute objectives are calculated independently from
the same evidence.

Availability ratios use `good_milliseconds / eligible_milliseconds`. Recovery
ratios use `qualifying_count / total_count`; duration fields retain the bounded
good, bad and excluded opportunity time for diagnosis. Aggregate links retain
the incident UUID and one of `failure`, `exclusion` or `recovery`. The
maintenance writer replaces those links atomically when it recomputes a row.
Every failed interval or finalized failed opportunity must carry a validated
incident link; the calculator rejects an unexplained failure. It stores no raw
event payload or error text.
