# Holding the Tunnel

The decision rule was proved against the runtime that owns the tunnel and
performed nothing: seven days, no disagreement. This is what changes when it
acts, what it does on its own, and how to take it back.

Taking the tunnel is `docs/macos/tunnel-configuration-version.md`. This document
is about the runtime that holds one.

## What the runtime needs to be able to act

An `execution` block inside `tunnel_supervision` in the root configuration.
Without it the runtime decides exactly as it did for the seven days and performs
nothing — which is the state it was installed in while it earned the right to
act, and a safe one to leave it in.

```json
"execution": {
  "version_path": "/Library/Application Support/Hexroute/observe-root/config/tunnel-version.json",
  "target_key": "<the node identity, as below>",
  "content_path": "/Library/Application Support/Hexroute/observe-root/state/tunnel-config.json",
  "sing_box": "/opt/homebrew/bin/sing-box",
  "handover_binary": "/Library/Application Support/Hexroute/observe-root/bin/hexroute-handover",
  "rebuilds_path": "/Library/Application Support/Hexroute/observe-root/state/tunnel-rebuilds.json",
  "bound_seconds": 30,
  "poll_seconds": 2
}
```

The node identity is the one the handover uses:

```sh
sudo cat '/Library/Application Support/Hexroute/observe-root/state/connectivity/node-id'
```

Every path is absolute and every bound is positive, and the daemon refuses to
start otherwise: a runtime that may stop the tunnel must not be one step from
starting nothing.

The runtime does not verify the signed version itself. It asks the handover
binary to raise the tunnel — the same verification, the same pinned key, the same
target — because that verification belongs to the delivery path and the
always-running daemons are kept off it. Stopping needs none of that and is the
daemon's own.

## The grant

Acting needs a generation that grants root-only `tunnel_ownership` for the
target `tunnel`. The ceremony is `docs/macos/policy-operations.md`; what is
particular here:

- The generation's validity has to outlast the seven days of ownership this
  change ends with, and the work before them. Measured 2026-09-25, the
  generation then in force expired in twelve days.
- Before the grant, every answer to the tunnel question is `selector_mismatch`:
  the question reaches policy and no rule selects the action. That is what an
  ungranted capability looks like from here.

After installing the executor and before the ceremony, read what the machine
answers:

```sh
sudo /usr/bin/python3 - <<'PY'
import glob, json, collections
answers = collections.Counter()
for path in glob.glob('/Library/Application Support/Hexroute/observe-root/state/event-archive/*.event'):
    try:
        record = json.load(open(path))
    except Exception:
        continue
    event = record.get('event') or {}
    if event.get('schema') != 'tunnel.decision':
        continue
    authorization = (event.get('payload') or {}).get('authorization')
    if authorization:
        answers['%s/%s' % (authorization.get('allowed'), authorization.get('reason'))] += 1
print(answers.most_common())
PY
```

`False/selector_mismatch` is the expected answer, and it is also the check that
the authorization is wired: a runtime whose handler was never given its control
state answers `control_state_unreadable` instead, which is a refusal about this
runtime rather than about the policy.

## What it does while it holds the tunnel

Every cycle it decides as before, records the decision, and then — if it holds
the claim, the machine is awake, the grant answers yes in that cycle and its own
rate bound allows it — rebuilds.

A rebuild stops the tunnel, asks the handover binary to raise one, puts back the
host routes that pointed at the old tunnel interface, and waits for traffic to
pass through the new one. It counts as done only then. What became of it is a
`tunnel.execution` record: performed with what it cost, or the gate that stopped
it.

It does not act while the machine is suspended. A dark wake lasts seconds, a
rebuild does not fit in one, and the gap keeps accruing: the first waking cycle
rebuilds instead.

It does not reconcile routes. The route planner goes on proposing what it
proposes and nothing applies it.

## What it does on its own

Three things end its ownership, and each runs the release transaction as a
giving-up — it does not take the tunnel back afterwards:

| | |
|---|---|
| the grant lapsed | the generation expired or was suspended |
| the rate bound | more than two rebuilds in five minutes, six in an hour or fifteen in a day |
| a tunnel carrying nothing | the payload path failed three consecutive complete cycles with the outer path reachable |

Each leaves word in
`/Library/Application Support/Hexroute/observe-root/state/tunnel-handback.json`,
and the user daemon announces it once as a critical notification. The archive
holds a `tunnel.handback` record saying whether the tunnel actually went back.

Telegram does not carry this yet: the cloud opens its own incidents by
reconciling silence and does not open one from a host's event. That is its own
change.

## When the tunnel has come back on its own

Read what happened:

```sh
sudo cat '/Library/Application Support/Hexroute/observe-root/state/tunnel-handback.json'
```

The reason says where to look. A lapsed grant is a ceremony; a rate bound is a
machine rebuilding without being repaired, and the decisions before it say what
it kept deciding; a tunnel carrying nothing with the outer path up is an ingress
this runtime cannot change, which is what the other runtime's failover is for.

Acting stays stopped until you clear it, and clearing it takes no tunnel:

```sh
sudo '/Library/Application Support/Hexroute/observe-root/bin/hexroute-handover' \
  --config '/Library/Application Support/Hexroute/observe-root/config/root-observe.json' \
  resume-executor
```

It says how many rebuilds it cleared and who holds the tunnel. Taking it back is
the handover, run by hand, and deliberately: an exchange interrupts traffic for
as long as it takes, and a runtime that took the tunnel back at a moment nobody
chose would do that while you were in the middle of something.

## Rolling back

Two levers, in the order of how much they undo.

Give the tunnel back by hand. It is the transaction the runtime uses, without
the giving-up, so it restores this runtime's tunnel if nobody else raises one:

```sh
sudo '/Library/Application Support/Hexroute/observe-root/bin/hexroute-handover' \
  --config '/Library/Application Support/Hexroute/observe-root/config/root-observe.json' \
  --tunnel-version '/Library/Application Support/Hexroute/observe-root/config/tunnel-version.json' \
  --target-key "$(sudo cat '/Library/Application Support/Hexroute/observe-root/state/connectivity/node-id')" \
  --sing-box /opt/homebrew/bin/sing-box \
  --content '/Library/Application Support/Hexroute/observe-root/state/tunnel-config.json' \
  release
```

Withdraw the grant. A generation without `tunnel_ownership` leaves the executor
unable to act while the daemon goes on observing and deciding, which is the state
the rule was proved in. It needs no code and no restart: the answer changes on
the next cycle.

Either can be rehearsed. `check-release` asks what a release would find, and
`rehearse` runs every phase of a handover except the two that change anything.

## Not covered here

- A carrier change shorter than a cycle. Both runtimes sample once a minute in
  different phases; measured 2026-09-23 and 2026-09-24, an ingress target left
  the upstream tunnel four times and was back before this runtime looked again.
  It does not rebuild for those, and the other runtime does.
- A dozing machine. Nothing is performed while it is suspended, by choice.
- Everything the other runtime still does: the Pritunl service, the OTP
  watchdog, reserve probes and failover selection, the Codex fallback path, and
  holding the machine awake.
