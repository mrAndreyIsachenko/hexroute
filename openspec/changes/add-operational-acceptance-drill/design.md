## Context

Hexroute has accumulated strong lower-level evidence: unit tests, race tests,
OpenSpec validation, synthetic replay, policy qualification and rollback
guards. Those checks prove internal invariants, but they do not prove the
operator can work after the failures Hexroute is supposed to survive.

The missing layer is an operational drill that spans user-visible paths:
Codex/ChatGPT, ordinary Internet access, GitLab web, Git SSH/Git push, Pritunl,
Twilight, AdGuard, Telegram/monitoring and fallback connectivity. Twilight
remains the production owner, so the drill must be read-only and coexist with
the current runtime.

## Goals / Non-Goals

**Goals:**

- Define one repeatable acceptance gate for "the operator can work".
- Collect redacted, non-secret evidence from read-only probes and explicit
  manual checkpoints.
- Cover baseline, post-activation, sleep/wake, reboot and network-loss recovery
  phases.
- Make the drill block future runtime/cutover work until failures are resolved
  or explicitly waived with evidence.
- Keep private targets, endpoints, Git remotes, session identities and raw logs
  outside public Git.

**Non-Goals:**

- Mutating routes, DNS, AdGuard, Pritunl, sing-box, launchd or cloud services.
- Replacing existing unit, race, replay or OpenSpec checks.
- Automating browser login, OTP entry, Telegram bot provisioning or provider
  console actions.
- Certifying production mutation readiness for Hexroute.

## Decisions

### The drill is a gate, not a monitor

The drill runs on demand before runtime/cutover work and after risky local
changes. It is not a daemon and does not retry, restart or repair anything.
This avoids creating a second recovery system while Twilight owns production.

### Read-only probes are separated from manual checkpoints

The script may run read-only checks such as HTTP reachability, DNS lookup,
route/status commands, `git ls-remote`/`ssh -T`-style Git transport checks and
local status inspection. Human checkpoints cover browser login, ChatGPT/Codex
message send, Pritunl OTP fallback and physical sleep/wake or reboot phases.

This split prevents a false claim that fully manual product behavior was
validated by a shell script.

### Evidence is redacted by shape

The public repository stores the script, schema and checklist, not raw evidence.
The script writes a local evidence bundle with status classes, timing buckets,
exit classes and redacted target labels. It must not persist full URLs with
tokens, IP addresses, hostnames, Git remotes, session IDs, cookies, OTP/PIN
values or command output that may contain credentials.

Private evidence can be retained outside Git when needed for incidents.

### The gate fails closed for core work paths

Core paths are Codex/ChatGPT reachability, ordinary Internet, GitLab web, Git
transport, Pritunl status/reconnect checkpoint, AdGuard coexistence and fallback
visibility. Any core path failure blocks the next runtime/cutover step unless a
bounded waiver records the failure, reason, owner and expiry.

### Runtime ownership stays unchanged

The drill can observe Twilight, AdGuard, Pritunl and Hexroute status, but it
cannot stop, start, restart, disable, reconfigure or assume ownership of them.
Future changes may make the drill mandatory for cutover, but this change only
defines and implements the non-mutating gate.

## Risks / Trade-offs

- **[False confidence from automated checks]** -> Manual checkpoints remain
  first-class and cannot be silently auto-passed.
- **[Sensitive evidence leaks into Git]** -> Public artifacts define redacted
  evidence shape only; secret-canary tests reject protected values and private
  target material.
- **[The drill becomes another recovery path]** -> It has no mutation authority,
  no restart commands and no launchd service.
- **[Too much manual work discourages use]** -> Keep the default smoke short,
  with deeper phases only for sleep/wake, reboot and network-loss drills.
- **[A transient outage blocks development]** -> Waivers are allowed only with
  explicit owner, reason, expiry and affected path.

## Migration Plan

1. Add the OpenSpec requirements, documentation and a read-only smoke script.
2. Add a documentation/secret-boundary test proving the drill is non-mutating
   and redacted.
3. Run the smoke script once in dry-run/local mode without touching live
   services.
4. Make the drill a prerequisite for future runtime/cutover OpenSpec changes.

Rollback removes the gate from the candidate change being prepared. Because the
drill performs no production mutation, rollback does not alter live networking.

## Open Questions

- Which private target labels should be mapped in `hexroute-infra` for the
  first real evidence run?
- Should Git push be a true temporary branch push in private infrastructure, or
  should the public drill stop at Git transport/write-permission verification?
- How long should a waiver remain valid for a known external outage?
