# Tasks

## 1. Authorize

- [x] 1.1 Add the Pritunl recovery capability to the policy model, so the authority arrives as a signed generation under user presence rather than as a build, a setting or a file.
- [x] 1.2 Give the mutation gate its first caller: both runtimes consult it, and the capability, before every act. Assert that an inactive, suspended or rolled-back generation removes the authority at once, and that the refusal is recorded as absent authority rather than as a failure.
- [x] 1.3 Assert no second path can permit the act. `credentials` and `pritunlrescue` leave the unwired census here, and nothing else may become a way in.

## 2. Act, Within The Boundary

- [x] 2.1 Reconnect through the client's password-read path. Assert that no PIN, seed or one-time code reaches an argument, an environment entry, a log line or an error — including when the client exits non-zero.
- [x] 2.2 Implement the root verifier over observations the root runtime already computes, and wire the handler. Assert that root reaches its own conclusion before acting, that it approves nothing but the one named service, and that it never sees a credential.
- [x] 2.3 Make inner health authoritative for the rescue request and for nothing else. Assert that a session reporting itself connected with an address and carrying no traffic requests a restart and attempts no reconnect on that ground, and that Pritunl's own report stays authoritative for reconnecting.

## 3. Take Ownership

- [x] 3.1 Write the transaction: disable the legacy watchdog, then activate the generation. Assert the order, and assert that disabling rather than booting out is what survives a reboot — an agent that returns at login would put two watchdogs on one profile with a thirty-second code window between them.
- [x] 3.2 Write the rollback: roll the generation back and the authority is gone; enable and bootstrap the legacy watchdog and it resumes. Neither step touches the Keychain, so nothing in the recovery path has to be sound for the recovery of the recovery path.
- [x] 3.3 Read the Keychain items where they are, and record in the cleanup item that Hexroute now depends on legacy-named items on a critical path.

## 4. Prove

- [ ] 4.1 (deferred to the cutover itself) Prove the user half by waiting. Reconnects run at 796 across 48 days with only six days seeing none, so a soak is a real sample, and the code-window and backoff paths appear in it on their own.
- [ ] 4.2 (deferred to the cutover itself) Prove the root half by inducing the precondition — a stale service — and not the request. A request written by hand proves the handler and skips the detection, and detection is the half this moves. 108 rescues across the same period, 90 of them in two days, is one incident rather than a rate: waiting for the next would mean holding an untested grant of root authority until an outage.
- [x] 4.3 Keep the evidence privately. The logs carry a live profile identity and a service label; neither enters this repository.

## 5. Keep Everything Else Untouched

- [x] 5.1 Assert the root tunnel, its supervisor, AdGuard and both Codex paths are unchanged, and that no capability beyond this one is granted by the same generation being active.

## 6. Verify

- [x] 6.1 Run `make check` and resolve every failure.
- [x] 6.2 Run `openspec validate cut-pritunl-recovery-to-hexroute --strict` and keep proposal, design, specs and tasks consistent with what was built.
- [ ] 6.3 (after the cutover) Sync the delta into the baseline specs and archive the change. It stays open while 4.1 and 4.2 do: archiving a change whose evidence has not been gathered would put a claim in the baseline that nothing supports.
