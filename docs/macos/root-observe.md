# Root Observe-Only Runtime

The root observe-only runtime is installed beside the production owner. It can
read macOS route, interface, power and process state and can perform bounded
TLS readiness checks. It cannot apply a route, restart a process, load a
configuration or alter another launchd job.

After each completed control-loop cycle it atomically advances
`state/control-loop.heartbeat.json`. The sequence and monotonic tick contain no
endpoint or credential values. A future sentinel measures how long that
sequence remains unchanged using its own monotonic clock.

GitLab HTTPS remains a scoped TUN route. GitLab SSH physical-path policy is a
separate `BindInterface` observation and is never represented as a competing
host route.

## Build And Validate

```sh
make build-observe-root
cp deploy/macos/root-observe.example.json private/root-observe.json
bin/hexrouted --check --config private/root-observe.json
```

Replace only the synthetic values in the untracked private configuration.
Never add the live file to Git.

## Install

```sh
sudo scripts/macos/observe-root-launchd.sh \
  install \
  bin/hexrouted \
  private/root-observe.json
```

The job uses the label `com.hexroute.observe.hexrouted`. Its binary, config,
state, socket namespace and logs are under candidate-only Hexroute paths.

```sh
sudo scripts/macos/observe-root-launchd.sh status
sudo scripts/macos/observe-root-launchd.sh logs
```

## Rollback

```sh
sudo scripts/macos/observe-root-launchd.sh uninstall
```

Uninstall removes only the candidate label and candidate paths. The production
owner remains authoritative throughout the observation period.
