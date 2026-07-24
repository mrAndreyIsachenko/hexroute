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

The user daemon also emits a bounded local notification when its Pritunl
planner enters `SAFE_MODE`. Notification delivery is best effort and cannot
stop the observation loop. See [`notifications.md`](notifications.md).

`pritunl_reconnect_proposed` means the candidate would have requested a
reconnect. It does not mean Hexroute performed one. Compare its timestamp with
the existing watchdog's recovery log while that watchdog remains active.

## Rollback

```sh
scripts/macos/observe-user-launchd.sh uninstall
```

Uninstall removes only the candidate user label and candidate paths. It does
not change the production recovery owner or any network component.
