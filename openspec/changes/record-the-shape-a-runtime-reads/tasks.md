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

- [ ] 1.3 Read whether the user domain's decoder reaches types the root one does not; verified by the set of wire types each reaches.

## 2. Carry the working copy forward

- [x] 2.1 Bring the user domain's working copy up to the machine; verified by a byte comparison and by refusing to overwrite anything the working copy holds alone.

      Carried forward 2026-10-08. The working copy held 30 of the machine's 31
      settings and nothing of its own, so the copy lost nothing; afterwards the
      two files are byte-identical. The previous version is kept beside the
      backup with its digest recorded.

- [ ] 2.2 Bring the root domain's working copy up to the machine, under the same refusal; verified by a byte comparison after it.

      This one needs a privileged read, so it is handed to the operator as a
      command. The comparison it runs first is the same shape as 2.1's: it
      refuses rather than overwrites when the working copy carries a setting the
      machine does not.

- [ ] 2.3 Record that this is a reading and not a guarantee; verified by the task saying what cannot be held true.

      `private/` is not in the repository, so nothing in this change can keep
      these two files in step with the machine. What the change can hold is that
      the *shape* is recorded, which is what makes a divergence visible without
      reading the host.

## 3. The record

- [ ] 3.1 Grow the root example to every setting its decoder accepts, with placeholder values; verified by the decoder accepting it.

- [ ] 3.2 Grow the user example the same way; verified the same way.

- [ ] 3.3 Say in both examples what they are: a record of the shape, with values chosen to decode rather than to run; verified by the line being there.

## 4. The gate

- [ ] 4.1 Walk each decoder's wire types by reflection over their `json` tags, including the types they reach into; verified by a test that the walk finds a setting known to be nested three deep.

      No list of settings beside the decoder. A list is the thing that was not
      updated.

- [ ] 4.2 Refuse a setting the decoder accepts and the example omits, naming it; verified by a test that removes a setting from a copy of the example and expects the refusal.

- [ ] 4.3 Refuse a setting the example carries and the decoder does not, naming it; verified by a test that adds one to a copy.

- [ ] 4.4 Refuse rather than skip a field the walk cannot resolve; verified by a test over a type the walk cannot enter.

- [ ] 4.5 Refuse a cycle rather than recursing; verified by a test over a recursive type.

- [ ] 4.6 Walk one element of a list and refuse an empty one; verified by a test over an example whose list is empty.

- [ ] 4.7 Prove the gate compares keys and never a value; verified by a test that changes every value in a copy of the example and expects the gate to pass.

- [ ] 4.8 Register the gate so it runs, and prove it fails on the parent commit; verified by its exit status before and after.

## 5. What an operator reads

- [ ] 5.1 Say in `docs/macos/root-observe.md`, where it already says the working copy is not the record, which comparison to run before an install and that the repository holds the shape; verified by the documentation gate.

- [ ] 5.2 Do the same for `docs/macos/user-observe.md`.

## 6. Mutation discipline

- [ ] 6.1 Mutate the walk and both refusals; verified by every survivor closed by a test or recorded with the reason it was left.

      A mutation that does not compile is rewritten, not counted.

## 7. Close

- [ ] 7.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

- [ ] 7.2 Prove no live value entered the repository; verified by `secret-test` and by reading the two examples by hand.

- [ ] 7.3 Sync the delta into the baseline, validate and archive; verified by the drift gate.

- [ ] 7.4 Record what this leaves open.
