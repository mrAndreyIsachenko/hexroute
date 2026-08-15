## Why

Hexroute has strong unit, race, replay, OpenSpec and synthetic qualification
checks, but it still lacks a first-class proof that the operator can keep
working after the exact failures this project is meant to survive. Before more
architecture or cutover work, we need an operational acceptance drill that
answers the user-level question: can Codex/ChatGPT, normal Internet, GitLab,
Git push, Pritunl and alerting still work after sleep, reboot and network loss?

## What Changes

- Add a public, provider-neutral operational acceptance capability that defines
  required probes, manual checkpoints, redacted evidence and pass/fail semantics.
- Add a local smoke drill script that collects non-secret evidence for normal
  Internet, Codex/ChatGPT reachability, GitLab web/SSH/Git, Pritunl/Twilight
  status, AdGuard coexistence, Telegram/monitoring status and fallback path
  visibility.
- Add separate drill phases for baseline, post-`make up`, sleep/wake, reboot
  and network-loss recovery.
- Make the drill an explicit gate before future runtime cutover, ownership
  transfer, production adapter activation or failover enablement.
- Keep private evidence, live endpoints, provider IDs, credentials and raw logs
  out of public Git; only redacted scripts, schemas, checklists and expected
  evidence shapes belong here.

Non-goals:

- Changing Twilight, AdGuard, Pritunl, sing-box, routes, DNS, launchd services
  or cloud infrastructure.
- Automating OTP entry, browser login, Telegram bot setup or provider actions.
- Treating a passed drill as permission to enable production mutations.
- Storing raw private hostnames, IPs, Git remotes, session IDs, credentials or
  operational evidence in this public repository.

Rollout starts with an offline/checklist-only drill and a read-only smoke
script. Rollback is removing the drill gate from the next candidate change; no
network inverse is needed because this change performs no production mutation.
Twilight remains the production owner throughout.

## Capabilities

### New Capabilities

- `operational-acceptance-drill`: Defines user-visible acceptance gates,
  read-only smoke probes, redacted evidence, manual recovery checkpoints and
  blocking semantics before future Hexroute runtime/cutover work.

### Modified Capabilities

- `local-control-plane-foundation`: Requires future runtime and cutover changes
  to preserve a passing operational acceptance drill before changing production
  ownership or enabling mutation authority.

## Impact

- Adds docs and scripts under the public Hexroute repository for a
  non-mutating, redacted acceptance drill.
- Adds OpenSpec requirements for acceptance evidence, manual checkpoints and
  cutover gating.
- Does not modify live runtime, provider resources, macOS launchd state,
  Twilight, AdGuard, Pritunl, sing-box, routes, DNS or either Codex access path.
