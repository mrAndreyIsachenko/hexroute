# Tasks

## 1. The ground

- [x] 1.1 Read how far each example has fallen behind, so the work is sized by a reading and not by the one divergence that was noticed; verified by the counts.

      Measured 2026-10-08. The root example carries 46 settings against the 112
      the installed configuration holds, and has no `tunnel_supervision` and no
      `policy_control` at all: it predates the policy control plane and the whole
      of tunnel observation. Ten of the settings it lacks are
      `tunnel_supervision.execution` entire — the executor
      `hold-the-tunnel-under-a-grant` built — which therefore exists only on one
      machine.

- [x] 1.2 Establish that the record can be both exhaustive and free of live material, before a requirement says so; verified by the decoder accepting a placeholder-only configuration.

      `tunnel_supervision` with its full `execution` block decodes with
      placeholders. `policy_control` does not, until the placeholders are
      self-consistent: `StaticConfig.Runtime` requires
      `SHA256Hex(pinned_public_key) == signer_fingerprint`. A zero ed25519 key
      with its own true digest decodes; an all-zero digest does not, which is
      what the first attempt got wrong.

      So there is no trade-off between an exhaustive example and the secret
      boundary, and the requirement that the example decode is satisfiable.

- [x] 1.3 Read whether the user domain's decoder reaches types the root one does not; verified by the walk over each decoder's wire types.

      It does, and the question was too narrow: there are **three** runtimes
      with an example, not two. `deploy/macos/sentinel-observe.example.json`
      exists and the sentinel has its own decoder.

      Each has wire types the others do not. All three declare their own
      `EndpointConfig` — three distinct types of the same name. Only root and
      user reach `policyconfig.StaticConfig`. The root alone reaches
      `TunnelSupervisionConfig`, `TunnelExecutionConfig`, `PayloadProbeConfig`
      and `RouteConfig`; the user alone reaches `PolicyConfig` and
      `RecoveryConfig`. So the gate's starting set is three type names, not two.

      Walked 2026-10-08, by settings the decoder accepts:

      | | accepted | in the example | missing | extra |
      |---|---|---|---|---|
      | root | 57 | 21 | **36** | 0 |
      | user | 44 | 23 | **21** | 0 |
      | sentinel | 12 | 12 | **0** | 0 |

      The sentinel's example is already complete. That is worth more than the
      two gaps: it is a domain the gate passes today, so the gate can be shown
      to accept as well as refuse, which a gate that only ever refuses cannot.

      These counts differ from the proposal's 46 against 112, and both are
      right about different things: that comparison was the example's leaf keys
      against the installed configuration's, this one is distinct json paths
      against what the decoder accepts. The gate uses this one, because the
      decoder is what it holds the example to.

## 2. Carry the working copy forward

- [x] 2.1 Bring the user domain's working copy up to the machine; verified by a byte comparison and by refusing to overwrite anything the working copy holds alone.

      Carried forward 2026-10-08. The working copy held 30 of the machine's 31
      settings and nothing of its own, so the copy lost nothing; afterwards the
      two files are byte-identical. The previous version is kept beside the
      backup with its digest recorded.

- [x] 2.2 Bring the root domain's working copy up to the machine, under the same refusal; verified by a byte comparison after it.

      This one needs a privileged read, so it was handed to the operator as a
      command. The comparison it runs first is the same shape as 2.1's: it
      refuses rather than overwrites when the working copy carries a setting the
      machine does not.

      Carried forward 2026-10-09. The working copy held 112 keys against the
      machine's 122 and nothing of its own, so the copy lost nothing: ten keys
      gained, none lost, and the two files are byte-identical afterwards. The
      ten are `tunnel_supervision.execution` entire and a second trusted
      compiler digest. `wake_threshold_seconds` went from 90 to 180, which is
      the machine's value and the one the tunnel soak ran under; the working
      copy's 90 was never anything but drift.

      Checked independently of the script's own report: the file is 0600 and
      owned by the operator, the execution block carries its nine settings, and
      the previous version is beside it as `root-observe.json.before-carry-forward`.

- [x] 2.3 Record that this is a reading and not a guarantee; verified by the task saying what cannot be held true.

      `private/` is not in the repository, so nothing in this change can keep
      these two files in step with the machine. What the change can hold is that
      the *shape* is recorded, which is what makes a divergence visible without
      reading the host.

      Nor does it stop the drift recurring: the comparison still has to be run.
      What changed is that running it now has a record to compare against, and
      that a setting cannot enter a runtime without entering the repository —
      which is the half that had no gate at all.

## 3. The record

- [x] 3.1 Grow the root example to every setting its decoder accepts, with placeholder values; verified by the decoder accepting it once trust material is supplied.

      From 21 settings to 57. Added `pritunl_service_label`,
      `expected_sing_box_parent_pid`, the whole of `tunnel_supervision`
      including `payload` and `execution`, and the whole of `policy_control`.
      Documentation addresses throughout, keeping the file's existing style.

- [x] 3.2 Grow the user example the same way; verified the same way.

      From 23 settings to 44: `policy_control` and the whole `recovery` section.
      The sentinel's example was already complete at 12 of 12 and was not
      touched, which is what lets the gate be shown accepting as well as
      refusing.

- [x] 3.3 Say what the examples are — but not in them; verified by the paragraph in both observe documents.

      Not possible as written, and the reason is this change's own rule. All
      three decoders use `DisallowUnknownFields`, and the gate refuses a key the
      decoder does not accept, so a `_comment` key would fail both the decoder
      and the gate. A record that cannot describe itself in its own file says so
      where it is referenced instead: `docs/macos/root-observe.md` already tells
      an operator to copy it. That is task 5.1 and 5.2, so this task is theirs.

## 4. The gate

- [x] 4.1 Walk each decoder's wire types by reflection over their `json` tags, including the types they reach into; verified by a test that the walk finds a setting known to be nested three deep.

      No list of settings beside the decoder. A list is the thing that was not
      updated. `internal/configshapeguard` holds the walk; the only written-down
      part is three type names, one per runtime.
      `TestTheWalkReachesASettingNestedThreeDeep` names four settings at
      different depths, including one inside a list element.

- [x] 4.2 Refuse a setting the decoder accepts and the example omits, naming it; verified by a test that removes a setting from a copy of the example and expects the refusal.

      `TestASettingTheExampleOmitsIsNamed` removes `tunnel_supervision` and
      requires the nested settings under it to be named, not just the block.

- [x] 4.3 Refuse a setting the example carries and the decoder does not, naming it; verified by `TestASettingTheDecoderDoesNotAcceptIsNamed`.

- [x] 4.4 Refuse rather than skip a field the walk cannot resolve; verified by `TestAFieldTheWalkCannotEnterIsRefused` over a struct holding an `any`.

- [x] 4.5 Refuse a cycle rather than recursing; verified by `TestATypeThatReachesItselfIsRefused`.

- [x] 4.6 Walk one element of a list and refuse an empty one; verified by `TestAListWithNoElementIsRefused`, and by the walk reaching `routes.preferred_link` through a list element.

- [x] 4.7 Prove the gate compares keys and never a value; verified by `TestTheComparisonIgnoresEveryValue`, which replaces every string, number and boolean in a copy and expects the same answer.

      Two more tests came out of the trust-material decision:
      `TestEveryExampleIsReadByItsOwnDecoderOnceTrustIsSupplied` and
      `TestTheExamplesHoldNoTrustMaterial`, which takes what to expect from the
      decoder rather than from the example, so a domain with no trust setting is
      checked for not carrying one instead of being skipped. The skip was the
      first thing written and `tests/hollow_green_test.sh` refused it.

- [x] 4.8 Register the gate so it runs, and prove it fails on the parent commit; verified by its exit status before and after.

      `configshapeguard` is in the `test_only` list of
      `tests/package_reachability_test.sh`, beside the three guards already
      there, so it is reachable by `go test` and recorded as not being in a
      binary.

      With the grown examples stashed, the gate exits **1** and reports the
      omissions for two of three domains; with them, **0**.

## 5. What an operator reads

- [x] 5.1 Say in `docs/macos/root-observe.md`, where it already says the working copy is not the record, which comparison to run before an install and that the repository holds the shape; verified by the documentation gate.

      It carries what the example is, that its values are placeholders chosen to
      decode rather than to run, the `--check --config --installed` comparison
      to run before installing rather than during, and the 2026-10-08 reading:
      21 of 57 settings, no `tunnel_supervision` and no `policy_control`, the
      whole executor on one machine.

- [x] 5.2 Do the same for `docs/macos/user-observe.md`, with its own reading: 23 of 44, missing `policy_control` and the whole `recovery` section.

## 6. Mutation discipline

- [x] 6.1 Mutate the walk and both refusals; verified by every survivor closed by a test or recorded with the reason it was left.

      A mutation that does not compile is rewritten, not counted. Twelve
      mutations, none uncompilable: reaching through pointers and through lists,
      descending into a nested struct, recording the block as well as its
      leaves, skipping an unresolvable field, allowing a cycle, honouring a
      `json:"-"` field, accepting an empty list, descending into an object, and
      three over the comparison.

      First run: 10 killed, 2 survived. Both survivors were untested behaviour
      rather than wrong behaviour:

      - **`json:"-"` was never exercised.** No wire type has such a field today,
        so counting one as a setting changed nothing. Closed by
        `TestAFieldTheDecoderIgnoresIsNotASetting` over a local type with one at
        two depths.
      - **The list-index stripping in `Compare` is unreachable from its own
        producer.** `DocumentKeys` files every element of a list under the same
        path and emits no index, so the expression never had anything to strip.
        It is kept, not deleted: `Compare` is exported, and one that reported
        every indexed path as unknown would be a worse trap than a line its
        producer never needs. The contract is now in the doc comment and held by
        `TestComparisonTakesAnIndexedKeyAsItsPath`.

      Second run: 12 killed, 0 survived.

## 7. Close

- [x] 7.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

      `make check` returned 0.

      Four gates did not run and none is applicable to this diff, which touches
      two example configurations, two documents, one new guard package and five
      tests: `postgres-test` (no migration or persistence change),
      `container-build` and `container-test` (no Dockerfile or ingest change),
      `terraform-test` and `terraform-state-test` (no module or fixture change),
      and the `policy-qualification` commands (nothing was installed and no
      runtime was restarted by this change). Docker and the Terraform CLI are
      both present, so the first three were skipped for scope, not
      availability.

- [x] 7.2 Prove no live value entered the repository; verified by `secret-test` and by reading the two examples by hand.

      `secretguard` and `repositoryguard` both pass, and the gate itself
      compares names only, which `TestTheComparisonIgnoresEveryValue` holds.

      Read by hand, not only by gate. Addresses are RFC 5737 documentation
      ranges and `.invalid` names; `physical_interface` is `en7` where this
      machine runs `en0`; the payload probe points at `example.invalid` where
      the machine points at a real host; the target key is an all-zero UUID;
      `static_sha256` and the compiler digest are all-zero hex; the pinned key,
      its fingerprint and `current_payload_sha256` are empty. The user domain's
      recovery block is synthetic throughout, which its own guard requires of
      `profile_id`.

      One thing in it is **not** a placeholder, and saying so is the point of
      reading by hand: the paths under `tunnel_supervision.execution` are the
      real installed paths. They are the same on every install, are already
      published in the documents and in the launchd plist, and name no host — so
      they are not deployment state. But they are real, and a reader should know
      that rather than assume everything here is invented.

- [ ] 7.3 Sync the delta into the baseline, validate and archive; verified by the drift gate.

- [x] 7.4 Record what this leaves open.

      **The comparison still has to be run.** Nothing here runs it: a gate
      cannot read the installed configuration, which only root may read, and
      `make check` runs as the operator. What changed is that the comparison now
      has a record to compare against, and that a setting cannot enter a runtime
      without entering the repository. Whether the reading should happen on a
      schedule, as the weekly archive review does, is its own question and was
      not answered here.

      **The private files are still only on this machine.** Both are carried
      forward as of 2026-10-09 and nothing holds them there.

      **Carried from the previous change, unchanged:** the read model's
      `scoped_routes` component reports `degraded` in every cycle including the
      sound ones, which is the same conflation one level down; nine of the ten
      causes a root cycle can name have not been seen on this machine; HEX-11's
      remaining question.

      **Found while reading the machine's state, not yet proposed:**
      `state/connectivity/readmodel/checkpoints` is bounded by nothing — 4290
      files and 336 MiB as of 2026-10-05, oldest 2026-08-31 — while both spools
      are held at 100 MiB each, so eviction runs on the spools and not on the
      checkpoints. That is distinct from HEX-19, which is the cost of eviction
      rather than its absence.

      **Older debts, untouched:** HEX-19, and there is still no path from a host
      event to an alert.
