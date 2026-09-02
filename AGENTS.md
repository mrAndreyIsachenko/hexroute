# Hexroute Engineering Workflow

Hexroute is replacing a working Twilight runtime incrementally. Treat the
installed Twilight configuration as an immutable production baseline until
the applicable migration, soak, fault-injection and rollback gates pass.

## Change Workflow

- Read `docs/roadmap.md` and the relevant baseline specs before planning a
  change.
- For a planned feature, reliability change or refactor, resolve decisions one
  at a time with `grill-me`, then create a repository-local OpenSpec proposal.
- Keep live provider identities, Terraform roots, deployment evidence and
  provider-specific operations in the private `hexroute-infra` repository.
- Keep Twilight compatibility and active local cutover requirements in the
  legacy Twilight repository until ownership has moved transactionally.
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

- Relevant repository-local OpenSpec tasks, baseline requirements and scenarios
  match the implementation.
- Unit, race, static and secret-leak checks pass for the affected packages.
- Live runtime changes are deployed only through an explicit guarded cutover.
- Affected normal and fallback paths are verified after deployment.
- Rollback is explicit and independently executable for connectivity changes.
- A hypothesis the work disproved is written down where the change is reviewed.
  What was ruled out, and by what evidence, is the half a commit message loses:
  the code records the answer that survived, and nothing records the two that
  did not, so the next person pays for the same measurement twice.
