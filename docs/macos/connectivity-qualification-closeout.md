# Closing out the connectivity qualification

Written before the soak ends rather than during it. The steps have an order,
several need root, and the one that matters is first: the evidence is captured
before anything is reinstalled.

None of this is reversible by rerunning it. A soak that has to start again
costs three days.

## 0. Know when it is done

```
sudo ./bin/hexroute-connectivity-replay \
  --qualification "/Library/Application Support/Hexroute/observe-root/state/connectivity-qualification" \
  --session <SESSION_UUID>
```

While it is running this exits non-zero with `"blocking": "not enough eligible
time"`. That is the gate refusing, not a failure. It is done when the same
command exits zero and prints `"passing": true`.

Eligible time is awake time. At roughly ninety per cent awake, the last
measurement put the finish about thirty-five hours out from 145,906 seconds.

## 1. Capture the evidence before touching anything

```
sudo ./bin/hexroute-connectivity-replay \
  --qualification "/Library/Application Support/Hexroute/observe-root/state/connectivity-qualification" \
  --session <SESSION_UUID> \
  | tee ".local/qualification/connectivity-$(date -u +%Y%m%dT%H%M%SZ).json"
```

`.local/` is untracked. The chain itself stays where it is — this is a copy of
what the gate said, at the moment it said it, and it is what task 9.4 is
recorded against.

Do this first. Every later step restarts something, and evidence captured
after a restart is evidence about a different run.

## 2. Tick 9.4 against the captured gate, not against a memory of it

The tool exits non-zero unless the gate passes, so a zero exit is the check.
Read `faults_missing`, `diverged` and `unbound` in the captured file: they must
be `null`, `0` and `0`. `guessed_healthy` must be `false`.

## 3. Run the gates, then sync and archive

```
make check
```

```
openspec validate add-observable-connectivity-state-machine --strict
```

Then 10.7: sync the delta requirements into the baseline specs and archive the
change. `openspec-sync-specs` does the first, `openspec-archive-change` the
second. Until that happens `make check` fails on the drift gate by design — a
completed change that is not archived is a baseline describing a system that no
longer exists.

## 4. Reinstall the root daemon

Two things change at once, and both are why this waits until now.

```
sudo scripts/macos/observe-root-launchd.sh install \
  ./bin/hexrouted \
  "/Library/Application Support/Hexroute/observe-root/config/root-observe.json"
```

**No session UUID this time.** Passing the old one would keep appending to a
finished chain; passing a new one would put two runs in a chain that then
describes neither. The qualification observer is done.

The installed plist now carries `--connectivity-event-archive`, so the archive
begins filling from the first cycle after this. That is the whole reason this
step is not optional: until it runs, the weekly review reports an empty window
and task 5.6 of the archive change stays open.

Check it came back:

```
sudo scripts/macos/observe-root-launchd.sh status
```

```
sudo ./bin/hexroute-connectivity-replay --state \
  --store "/Library/Application Support/Hexroute/observe-root/state/connectivity"
```

The second is the one worth reading. A daemon that started and a read model
that resumed are different claims.

## 5. Let the review find something

The weekly job runs on its own interval and will pick up the archive without
help. To see it sooner:

```
sudo launchctl kickstart -k system/com.hexroute.observe.archive-review
```

```
sudo scripts/macos/archive-review-launchd.sh attempts
```

`finished exit=0` is the first real report. Before the daemon was reinstalled
this line read `exit=2`, and that difference is the whole point of recording
the attempt separately from the outcome.

```
sudo scripts/macos/archive-review-launchd.sh reports
```

## What is still open afterwards

- `add-private-incident-bundles` waits on object storage configured in
  `hexroute-infra`. Nothing local unblocks it.
- `observe-sentinel-recovery` waits on the sentinel being installed, which
  waits on a private configuration naming what to probe through Twilight and
  how often.
- The unwired list is unchanged by any of this. It is 1425 lines across five
  packages, each held behind its own cutover.
