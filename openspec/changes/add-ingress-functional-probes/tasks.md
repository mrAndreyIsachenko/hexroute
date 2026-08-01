## 1. Probe Contracts

- [x] 1.1 Add strict stdin request decoding, validation and a redacted versioned result envelope with stable failure categories.
- [x] 1.2 Implement independently bounded TCP and public TLS-fallback probes.
- [x] 1.3 Implement strict signed-heartbeat response parsing and authenticity, freshness, generation and runtime-health validation.

## 2. Authenticated Transport

- [x] 2.1 Render a minimal loopback-only sing-box VLESS/Reality configuration from validated in-memory input.
- [x] 2.2 Run the HTTPS canary through temporary SOCKS with bounded readiness, child termination and private-file cleanup.
- [x] 2.3 Add the standalone `hexroute-ingress-probe` command without local daemon, route, DNS, Keychain or provider mutation authority.

## 3. Regression Evidence

- [x] 3.1 Add deterministic TCP, TLS and signed-heartbeat success/failure tests.
- [x] 3.2 Add authenticated-runner tests for configuration modes, loopback-only behavior, deadlines and cleanup.
- [x] 3.3 Add CLI fixtures proving request values and dependency errors cannot leak to stdout, stderr or process arguments.

## 4. Documentation And Coordination

- [x] 4.1 Document probe semantics, secret input ownership and the distinction between fallback and authenticated evidence.
- [x] 4.2 Run formatting, unit, race, static, secret, strict OpenSpec and full repository checks.
- [x] 4.3 Publish the public revision, pin it in private policy, mark parent task 4.3 complete and record pre-adoption rollback.
