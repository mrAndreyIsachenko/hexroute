# Hexroute Engineering Workflow

Hexroute is replacing a working Twilight runtime incrementally. Treat the
installed Twilight configuration as an immutable production baseline until
the applicable migration, soak, fault-injection and rollback gates pass.

## Change Workflow

- Implement behavior from the approved OpenSpec change in the legacy Twilight
  repository: `build-hexroute-control-plane`.
- Keep implementation commits small and map each one to explicit OpenSpec
  tasks and regression evidence.
- Run the repository checks before marking a task complete.

## Safety Invariants

- Never stop, disable or reconfigure AdGuard.
- Preserve both the normal and Twilight paths to Codex.
- Never route a Twilight ingress through Twilight's own TUN.
- Never restart VPS XRay solely from a failed Mac-side probe.
- Keep Pritunl PIN/TOTP and VLESS/Reality/MTG secrets out of Git, command
  arguments, logs, telemetry and test fixtures.
- Preserve root network/process ownership and user Keychain/OTP ownership.
- Keep Hexroute paths, launchd labels, state, sockets and logs disjoint from
  Twilight until transactional cutover.
- Cloud components are telemetry-only and cannot request local mutations.

## Definition Of Done

- Relevant OpenSpec tasks and scenarios match the implementation.
- Unit, race, static and secret-leak checks pass for the affected packages.
- Live runtime changes are deployed only through an explicit guarded cutover.
- Affected normal and fallback paths are verified after deployment.
- Rollback is explicit and independently executable for connectivity changes.
