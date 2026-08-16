# Policy Shadow Qualification

The policy qualification agent converts live, redacted root and user policy
status into the private hash-linked evidence required before `operator_resume`
enforcement can be considered. It is a user LaunchAgent with no policy
activation, route, DNS, network, credential or process-mutation interface.
Twilight remains the production owner throughout qualification.

## Install And Start

Both observe-only daemons must already report one confirmed active bundle. Build
and install the disjoint agent as the login user, without `sudo`:

```sh
make install-policy-qualification
make policy-qualification-status
```

Installation starts a session only when no current state exists. Reinstalling a
new binary preserves and resumes the current private session. Evidence lives
under `~/Library/Application Support/Hexroute/policy-qualification` with mode
`0700`; source and state files use mode `0600`. Logs are under
`~/Library/Logs/Hexroute/policy-qualification` and contain no policy source,
endpoint, selector, credential or test-report content.

The first status is `collecting`. Its binding must match the active root and
user bundle, their independent domain generations and the manifest digest.
`eligible_seconds` increases only while samples remain contiguous and the exact
binding stays active and unsuspended.

## Record Controlled Faults

Run the four focused, deterministic rejection tests once during the live
session:

```sh
make policy-qualification-faults
make policy-qualification-status
```

The operator-run script tests invalid signatures, selector conflicts, stale
generations and crash-between-domain-commit recovery. Raw test output is
temporary. The private source record retains only its SHA-256 digest, typed
outcome, boot/session binding and observation time.

## Record Sleep And Reboot

Immediately before closing the lid, arm one cycle:

```sh
make policy-qualification-arm-sleep
```

After wake, the running agent counts the cycle only when the same agent run,
boot ID and policy binding remain valid, the Darwin continuous monotonic clock
matches elapsed UTC time, and macOS reports an open lid and full wake. An
armed cycle remains pending through intermediate macOS dark wakes and a short
pre-sleep sampling race; regular samples do not clear the explicit arm. The arm
expires after 24 hours and is consumed only by a validated full wake. Pending
observations neither count the cycle nor invalidate it. An unarmed long
scheduler gap or an agent relaunch still invalidates the session. Repeat this
procedure for the second required cycle.

An invalidated session retains any rejected arm as non-authoritative forensic
metadata. It can explain a failed wake decision but cannot authorize or count a
later cycle; only a new collecting session can be armed.

Perform one ordinary reboot while the agent is installed. On launchd restart,
the agent revalidates the complete chain and source store, records the boot UUID
transition and begins a new eligible segment. Reboot downtime is accounted for
but is not added to `eligible_seconds`.

If reboot remains missing after the Mac has actually restarted, do not restart
the qualification session. First verify that the root observe socket exists at
`/var/run/hexroute-observe/hexrouted.sock` and that
`com.hexroute.observe.hexrouted` is running. The socket parent is volatile across
macOS reboot and `hexrouted` is responsible for recreating it on daemon start.

## Interpret And Recover

```sh
make policy-qualification-status
make policy-qualification-summary
make logs-policy-qualification
```

`policy-qualification-summary` prints the gate checklist, elapsed eligible
time, missing sleep/wake, reboot and fault criteria, and the next operator
actions. Use it before restarting a session; `restart-session` discards all
elapsed qualification time from the current session.

| Lifecycle | Meaning |
|---|---|
| `collecting` | Chain and sources are valid, but one or more duration, lifecycle or fault criteria remain incomplete |
| `invalid` | Binding, timing, source, clock or chain validation failed; no elapsed time from this session can authorize enforcement |
| `complete` | Durable replay passed all criteria, including 72 eligible hours, two armed sleep/wake cycles, one reboot and four fault outcomes |

When the lifecycle reaches `complete`, the LaunchAgent process exits
successfully and launchd does not restart it. The private evidence and
`policy-qualification-status` remain available. A non-zero exit is still
restarted by launchd so collection failures stay visible during active
qualification.

Correct the cause of an invalid session, then explicitly begin a new one:

```sh
make policy-qualification-restart-session
```

The old session directory is preserved for private diagnosis; elapsed time is
never carried forward. Do not edit `current.json`, `qualification.jsonl` or a
source file. Any rewrite makes replay fail closed.

To roll back the recorder without affecting connectivity:

```sh
make uninstall-policy-qualification
```

This removes only the qualification LaunchAgent and installed binary. It does
not stop or reconfigure Hexroute observers, Twilight, AdGuard, Pritunl,
sing-box, routes or either Codex path, and it preserves all private evidence.
