# Sentinel Observe-Only Runtime

`hexroute-sentinel` independently reads the root control-loop heartbeat and
performs a bounded TLS handshake through the legacy Twilight SOCKS5 data path.
It tracks heartbeat progress with its own monotonic clock, so wall-clock
changes cannot manufacture a stale-loop signal.

The initial runtime is strictly observe-only. A stale heartbeat alone and a
failed data-path probe alone are insufficient. When both signals persist, the
sentinel records `sentinel_evidence`.

It also runs the planner that would decide the recovery, and records what an
authorized sentinel would have done. That is the difference between knowing
the evidence was there and knowing what would have followed from it: the
evidence line repeats for as long as the condition holds, while the plan moves
through phases and stops.

| Record | What it says |
| --- | --- |
| `sentinel_recovery_plan` | the phase the planner is in and the action it selected, written when either changes rather than every cycle |
| `sentinel_recovery_bound` | the point at which an authorized sentinel would have spent its one permitted attempt and stopped |
| `sentinel_planner_unavailable` | the planner refused an input; the sentinel keeps watching and stops planning |

| Phase | What it means |
| --- | --- |
| `monitoring` | watching; no action selected |
| `verifying` | an authorized sentinel would have acted and would now be checking whether it worked |
| `cooldown` | the one attempt is spent; nothing further until the window passes |

`sentinel_recovery_bound` is the number a cutover decision needs. Over a long
observation it says how often the sentinel would have restarted the root
daemon and whether it would have stopped when it should — which is not
derivable from the evidence line, because that line says only that the gate
was met.

The observing sentinel holds no means of acting. Its controller is built with
no restarter at all rather than one it declines to use, so there is nothing to
call: not acting is a property of the object and not of a branch.

The compiled active-control boundary accepts only the fixed root candidate
launchd target, one attempt, a verification window and a cooldown of at least
ten minutes. The observe-only command does not construct that adapter.
Activation remains a separate cutover step after stale-loop fault testing.

```sh
go build -o bin/hexroute-sentinel ./cmd/hexroute-sentinel
bin/hexroute-sentinel \
  --check \
  --config deploy/macos/sentinel-observe.example.json
```

The synthetic example must be replaced by an untracked private config before a
live observation. No route, process, service or credential identifiers other
than the candidate heartbeat path belong in the public file.
