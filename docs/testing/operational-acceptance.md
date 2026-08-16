# Operational Acceptance Drill

The operational acceptance drill proves the practical work path: after a
candidate runtime state, sleep, reboot or network interruption, the operator can
still use Codex/ChatGPT, ordinary Internet, GitLab, Git transport, Pritunl,
Telegram/monitoring and a fallback path.

This drill is a gate, not a recovery tool. It does not stop, start, restart,
disable, reconfigure or assume ownership of Twilight, AdGuard, Pritunl,
sing-box, routes, DNS, launchd services, provider resources or cloud services.

## Phases

Run the drill independently for every required phase:

- `baseline`: before changing local runtime state.
- `post-activation`: after a candidate local activation or `make up` style
  action performed by the owning project.
- `post-sleep`: after closing and reopening the Mac.
- `post-reboot`: after a normal reboot.
- `post-network-loss`: after losing and restoring network access.

Passing one phase does not pass another phase. Every phase needs fresh evidence.

## Core Work Paths

The acceptance gate covers these paths:

- `ordinary_internet`: a normal public web target.
- `codex_chatgpt_http`: ChatGPT/Codex reachability.
- `gitlab_web`: GitLab web reachability.
- `git_transport`: `git ls-remote` with prompts disabled.
- `pritunl_process`: local Pritunl process visibility.
- `pritunl_profile`: read-only Pritunl profile connection state; a running
  Pritunl service alone is not accepted as a connected corporate tunnel.
- `adguard_process`: local AdGuard process visibility.
- `twilight_status`: read-only Twilight status, when a Twilight directory is
  configured.
- `telegram_monitoring`: monitoring or Telegram alert surface reachability.
- `fallback_path`: independent fallback path reachability.

Any failed or `not_configured` core path blocks the next runtime cutover or
mutation-enabling step unless there is an explicit bounded waiver.

## Private Targets

Targets are configured outside Git. The default file is ignored by this
repository:

```sh
private/acceptance-targets.env
```

Supported keys:

```sh
HEXROUTE_ACCEPTANCE_URL_INTERNET=https://example.invalid/
HEXROUTE_ACCEPTANCE_URL_CODEX=https://example.invalid/
HEXROUTE_ACCEPTANCE_URL_GITLAB=https://example.invalid/
HEXROUTE_ACCEPTANCE_GIT_REMOTE=git@example.invalid:group/repo.git
HEXROUTE_ACCEPTANCE_PRITUNL_PROFILE_ID=profile-id
HEXROUTE_ACCEPTANCE_PRITUNL_CLI=/Applications/Pritunl.app/Contents/Resources/pritunl-client
HEXROUTE_ACCEPTANCE_TWILIGHT_DIR=/path/to/twilight
HEXROUTE_ACCEPTANCE_URL_MONITORING=https://example.invalid/
HEXROUTE_ACCEPTANCE_URL_FALLBACK=https://example.invalid/
```

The script parses this file as `KEY=VALUE` data and does not execute it as a
shell script. If a target is missing, the related check is `not_configured`;
the script does not guess endpoints or inspect other private configuration.
The Pritunl profile probe runs `pritunl-client list` only when
`HEXROUTE_ACCEPTANCE_PRITUNL_PROFILE_ID` is configured. It stores only bounded
result classes such as `connected`, `not_connected` or `profile_not_found`; raw
Pritunl output, profile names and client addresses are not written to evidence.

## Manual Checkpoints

Some checks must remain manual because the acceptance criterion is user-visible
behavior, not just an HTTP response:

- Browser can open ChatGPT/Codex and the operator can send a message.
- GitLab can be used in the browser.
- Git write path is verified by the operator, or by private infrastructure with
  a temporary branch if that is later approved.
- Pritunl can connect through the normal OTP fallback path.
- The post-sleep phase followed a real sleep/wake cycle.
- The post-reboot phase followed a real reboot.
- The post-network-loss phase followed real loss and restoration of network
  access.
- For `post-sleep`, `post-reboot` and `post-network-loss`, no out-of-band
  manual rescue path had to be enabled before the drill. If the operator had to
  enable an external VPN such as Amnezia to regain access, the phase is not a
  self-recovery pass.

Manual checkpoints are confirmed with environment variables:

```sh
HEXROUTE_ACCEPTANCE_MANUAL_CHATGPT_BROWSER=pass
HEXROUTE_ACCEPTANCE_MANUAL_CODEX_MESSAGE=pass
HEXROUTE_ACCEPTANCE_MANUAL_GIT_WRITE=pass
HEXROUTE_ACCEPTANCE_MANUAL_PRITUNL_OTP=pass
HEXROUTE_ACCEPTANCE_MANUAL_SLEEP_WAKE=pass
HEXROUTE_ACCEPTANCE_MANUAL_REBOOT=pass
HEXROUTE_ACCEPTANCE_MANUAL_NETWORK_LOSS=pass
HEXROUTE_ACCEPTANCE_MANUAL_NO_EXTERNAL_RESCUE=pass
```

Allowed values are `pass` and `fail`. Missing values are recorded as
`incomplete` and cannot be counted as a passing drill.

Use `HEXROUTE_ACCEPTANCE_MANUAL_NO_EXTERNAL_RESCUE=pass` only when the tested
state recovered without enabling an out-of-band rescue path. If manual Amnezia
or another external fallback had to be enabled first, omit the variable or set
it to `fail`; the resulting evidence can still document operator recovery, but
it cannot be cited as Hexroute/Twilight self-recovery.

## Evidence

Run a dry plan first:

```sh
scripts/ops/acceptance-smoke.sh --dry-run --phase baseline
```

Run a real read-only smoke phase:

```sh
HEXROUTE_ACCEPTANCE_TARGETS=private/acceptance-targets.env \
  scripts/ops/acceptance-smoke.sh --phase baseline
```

Evidence is written to `.local/acceptance` by default. That directory is
ignored by Git. The bundle contains only:

- schema and phase;
- UTC timestamp;
- dry-run flag;
- overall result: `pass`, `blocked`, `incomplete` or `dry_run`;
- target-file-present flag;
- check labels, kinds, pass/fail classes, timing buckets and exit classes;
- manual checkpoint labels and statuses.

HTTP smoke probes treat a received `2xx`, `3xx` or `4xx` response as reachable:
login pages, redirects, auth challenges and bot-gates still prove that DNS,
routing and TLS reached the target. `5xx`, DNS errors, connect errors, timeouts
and missing responses remain failures. User-visible login or message-send
behavior stays in the manual checkpoints.

It must not contain raw private hostnames, IP addresses, URLs with tokens, Git
remotes, session identifiers, cookies, OTP/PIN values, credentials, command
stdout that may include secrets or raw logs.

## Waivers

A failed core path blocks the next runtime/cutover step. A waiver is allowed
only when the failure is understood and bounded. Keep waiver evidence outside
Git with:

- affected path;
- owner;
- reason;
- expiry;
- mitigation;
- link to private evidence.

A waiver does not grant production mutation authority and does not replace the
candidate change's own safety, qualification or rollback requirements.

## Runtime Boundary

Passing this drill means the operator work path survived the tested phase. It
does not enable a production adapter, cut ownership from Twilight, change policy
generation, alter cloud authority or approve failover. Every such step still
requires its own OpenSpec change, safety envelope and independently executable
rollback.
