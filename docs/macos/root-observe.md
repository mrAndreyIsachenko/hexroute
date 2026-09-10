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

Set `operator_uid` to the numeric UID of the login user allowed to query the
root typed socket. The root-owned socket directory remains non-writable by that
user.

When `policy_control` is present, initialize the fixed root policy store before
activation and install signed artifacts only through the guarded installer:

```sh
sudo bin/hexroute-policy-installer init --domain root
sudo bin/hexroute-policy-installer install \
  --domain root \
  --candidate '<private canonical candidate directory>' \
  --signed '<private signed review directory>'
```

Initialization pins newly created store directories to `root:wheel` and mode
`0700`, even when the enclosing application directory uses another group. An
existing insecure store is rejected rather than repaired implicitly. Artifact
installation does not activate a policy or touch the network.

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

Build `hexroutectl` and use the commands in
[`operator.md`](operator.md) for typed status and redacted diagnostics.
With no active pointer the daemon stays available in observe-only `SAFE_MODE`
and reports `none/no_valid_generation`; this is fail-closed and does not affect
Twilight or AdGuard.

### The configuration in the working copy is not the record

The installer judges what you hand it against what is already installed and
refuses a configuration that drops a setting the live one carries. A stale
working copy is the ordinary way this happens: on 2026-09-10 both daemons were
installed from checkout files that had lost `policy_control`, and root's had
also lost `pritunl_service_label`. Both files were valid. Two runtimes holding
a signed generation stopped holding one, and every check in the path reported
success.

A refusal lists what would be lost and changes nothing:

```
would lose policy_control
would lose policy_control.pinned_public_key
would lose pritunl_service_label
```

Read the installed configuration, carry those settings into the file you are
installing, and run again. When the reduction is deliberate — stepping a domain
back to no authority is a real operation — say so:

```sh
HEXROUTE_ALLOW_REDUCED_CONFIG=1 sudo scripts/macos/observe-root-launchd.sh install bin/hexrouted private/root-observe.json
```

Each install keeps the configuration it replaced beside the installed one as
`root-observe.json.replaced`. It is one copy, not a history: it answers what was replaced
just now.

## Rollback

```sh
sudo scripts/macos/observe-root-launchd.sh uninstall
```

Uninstall removes only the candidate label and candidate paths. The production
owner remains authoritative throughout the observation period.
