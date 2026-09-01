## 1. Archive Store

- [x] 1.1 Add an append-only local archive keyed by a monotonic sequence, using the existing staged-write, file-sync, atomic-rename and directory-sync discipline.
- [x] 1.2 Accept only records that decode under a registered event schema; refuse anything else and record the refusal as a bounded diagnostic rather than dropping it.
- [x] 1.3 Enforce a configured maximum age and maximum total size, evicting lower priorities first for size and ignoring priority for age.
- [x] 1.4 Emit a durable overflow record naming the class of records dropped and the sequence range they covered, and refuse an append that could only be satisfied by silently discarding a critical record.
- [ ] 1.5 Add crash-point tests at every write boundary, plus staged-file recovery, truncation, corrupted-record and bound-exhaustion cases.
- [x] 1.6 Prove archiving and upload are independent in both directions: acknowledgement never evicts an archived record, and archiving never causes an upload.
- [ ] 1.7 Connect the archive to a producer. Nothing writes to it today, and nothing can: the host emits no typed event stream, because `internal/telemetry` — which owns upload and the acknowledgement that empties the spool — is reachable only from `cmd/hexroute-ingest`, and `telemetry.NewUploader` is called from tests and nowhere else. The one durable event producer on this machine is the connectivity journal, and it cannot gain a second sink while the qualification soak is measuring it. Until this lands the archive is reachable from no binary and the reachability census reports it as such.

## 2. Read And Aggregation

- [ ] 2.1 Add a bounded read API over a requested window that reports the window actually covered, including the empty case.
- [ ] 2.2 Add deterministic aggregation over a window: counts by schema and component, observed transition sequences, and a rarity ranking with a documented tie-break.
- [ ] 2.3 Add a report value with a canonical encoding and digest so two runs over one archive are comparable byte for byte.
- [ ] 2.4 Add tests proving equal archives yield equal reports, a shortened window is reported as shortened, and an empty window is never rendered as a quiet healthy one.

## 3. Optional Model Commentary

- [ ] 3.1 Add an off-by-default local model pass that receives the finished report and may only attach commentary to findings that already exist.
- [ ] 3.2 Discard unparsable output and reject commentary referencing a finding that is not in the report.
- [ ] 3.3 Prove the ordered findings are identical with and without the model, and that only the commentary field differs.
- [ ] 3.4 Prove the report is complete and valid when the model is absent, times out or returns nonsense, and that its absence is recorded.

## 4. Report Command And Schedule

- [ ] 4.1 Add a report command that writes a dated report for a requested window and nothing else.
- [ ] 4.2 Add a weekly local schedule that owns its own interval, records the attempt before making it, and always exits successfully.
- [ ] 4.3 Prove the review performs no network, credential, privileged or mutating operation, and that a failed review leaves the archive and every production path unchanged.
- [ ] 4.4 Remove the schedule cleanly on request, scoped to the paths the review installed.

## 5. Verification And Documentation

- [ ] 5.1 Document the archive's retention contract, its bounds, what an overflow record means, and how the covered window is read.
- [ ] 5.2 Document why raising the spool bound does not provide retention, so the distinction is not lost the next time the question is asked.
- [ ] 5.3 Run focused unit, race, crash-recovery and secret-canary tests for the affected packages.
- [ ] 5.4 Run `make check` and resolve every static, race, formatting, repository-boundary and secret-leak failure.
- [ ] 5.5 Run `openspec validate add-local-event-archive --strict` and keep proposal, specs and tasks synchronized with implementation evidence.
- [ ] 5.6 Sync validated delta requirements into baseline specs only after implementation and a first real weekly report exist.
