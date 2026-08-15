## 1. Acceptance Contract And Evidence Shape

- [ ] 1.1 Define the operational acceptance phases, required work paths, pass/fail classes and waiver semantics.
- [ ] 1.2 Define a redacted evidence bundle format that excludes private endpoints, Git remotes, session identifiers, credentials, OTP/PIN values and raw logs.
- [ ] 1.3 Add a private-target input contract with labels only and fail-closed `not_configured` behavior.

## 2. Non-Mutating Smoke Drill

- [ ] 2.1 Add a read-only smoke script for Internet, Codex/ChatGPT, GitLab web, Git transport, Pritunl/Twilight/AdGuard coexistence, Telegram/monitoring and fallback visibility checks.
- [ ] 2.2 Add dry-run and phase selection support for baseline, post-activation, post-sleep, post-reboot and post-network-loss evidence.
- [ ] 2.3 Prove the script contains no service-control, route-mutation, DNS-mutation, provider-mutation or credential-access commands.

## 3. Manual Checkpoints And Operator Docs

- [ ] 3.1 Document browser, Codex/ChatGPT message, Pritunl OTP fallback, sleep/wake, reboot and network-loss manual checkpoints.
- [ ] 3.2 Document how acceptance evidence blocks future runtime/cutover work and how bounded waivers are recorded.
- [ ] 3.3 Link the drill from README and roadmap as the next gate before observable state-machine implementation.

## 4. Verification And Sync

- [ ] 4.1 Add shell/documentation tests for drill coverage, redaction, non-mutation and README/roadmap links.
- [ ] 4.2 Run focused shell tests and `openspec validate add-operational-acceptance-drill --strict`.
- [ ] 4.3 Run `make check` and resolve every static, race, formatting, repository-boundary and secret-leak failure.
- [ ] 4.4 Sync validated delta requirements into baseline specs only after implementation is complete; do not claim production mutation readiness.
