# User Observe-Only Runtime

`hexroute-userd` runs beside the current Pritunl recovery owner. It reads the
console session, clamshell state, Pritunl profile/service state and outer
Twilight readiness, then records the action its bounded policy would propose.
It cannot connect or disconnect Pritunl, restart a service, read Keychain or
submit a PIN or OTP.

The candidate uses the launchd label `com.hexroute.observe.userd` and stores
its binary, config, state and logs only under Hexroute `observe-user` paths.
The existing OTP watchdog remains authoritative until a separate active-control
cutover and rollback gate are approved.

## Build And Validate

```sh
make build-observe-user
cp deploy/macos/user-observe.example.json private/user-observe.json
bin/hexroute-userd --check --config private/user-observe.json
```

Replace only synthetic observation identifiers and policy values in the
untracked private configuration. Credentials and Keychain item names do not
belong in this file.

When `policy_control` is present, initialize and populate the user policy store
as the login user, never with `sudo`:

```sh
bin/hexroute-policy-installer init --domain user
bin/hexroute-policy-installer install \
  --domain user \
  --candidate '<private canonical candidate directory>' \
  --signed '<private signed review directory>'
```

Installation revalidates a confirmed active generation before accepting a
later parent generation. It writes immutable policy artifacts but does not
change the active pointer, access Keychain or reconnect Pritunl.

## Install

Run the installer as the login user, without `sudo`:

```sh
scripts/macos/observe-user-launchd.sh \
  install \
  bin/hexroute-userd \
  private/user-observe.json
```

Inspect only redacted candidate decisions:

```sh
scripts/macos/observe-user-launchd.sh status
scripts/macos/observe-user-launchd.sh logs
```

The installer also creates the private `state/userd.sock` operator endpoint.
Build `hexroutectl` and use the commands in
[`operator.md`](operator.md) to inspect typed state. Explicit resume only
changes a persisted candidate `SAFE_MODE` snapshot back to `DEGRADED`; in
observe-only mode it does not connect Pritunl.

Root and user daemons must confirm the same bundle before policy-authorized
state transitions are eligible. Their domain policy generation numbers may
differ when only one domain's effective payload changed.

The user daemon also emits a bounded local notification when its Pritunl
planner enters `SAFE_MODE`. Notification delivery is best effort and cannot
stop the observation loop. See [`notifications.md`](notifications.md).

`pritunl_reconnect_proposed` means the candidate would have requested a
reconnect. It does not mean Hexroute performed one. Compare its timestamp with
the existing watchdog's recovery log while that watchdog remains active.

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
HEXROUTE_ALLOW_REDUCED_CONFIG=1 scripts/macos/observe-user-launchd.sh install bin/hexroute-userd private/user-observe.json
```

Each install keeps the configuration it replaced beside the installed one as
`user-observe.json.replaced`. It is one copy, not a history: it answers what was replaced
just now.

## Rollback

```sh
scripts/macos/observe-user-launchd.sh uninstall
```

Uninstall removes only the candidate user label and candidate paths. It does
not change the production recovery owner or any network component.
