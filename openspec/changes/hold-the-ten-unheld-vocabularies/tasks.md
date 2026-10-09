# Tasks

## 1. The ground

- [x] 1.1 Read where each of the ten is explained today, so the work is sized by what each needs rather than by their number; verified by the table in the proposal.

      Measured 2026-10-09. Seven are already explained in the place that owns
      them and about the thing they are: `AuthorizationReason` as its own rows
      in `## Authorization`, and six class vocabularies inside their payload
      field's own rows. They need a check and nothing else.

      Three do not. `Reason` has no place and ten of its twelve values appear
      nowhere in the document. `Lifecycle` and `AggregateState` have no place of
      their own.

- [x] 1.2 Establish that two of them would pass a section-keyed check for the wrong reason, rather than assuming the previous change's pattern covers them; verified by the value sets.

      `Lifecycle` (5 values) and `AggregateState` (4) are **strict subsets** of
      `ComponentState` (7), and the rows of `### Component states` are exactly
      `ComponentState`'s seven. A check keyed to that section finds every
      lifecycle and aggregate value and is satisfied by rows about the derived
      state — a third thing.

      So the pattern that fixed the last two changes does not reach this case,
      and finding that out before writing the checks is what this task was for.

## 2. Three vocabularies, three tables

- [ ] 2.1 Say in one sentence, before the tables, that three vocabularies share these words and what distinguishes them; verified by the sentence naming all three roles.

- [ ] 2.2 Give what a collector asserted its own table — `Lifecycle`, five values, read from the mappers that emit each; verified by the gate keyed to its subheading.

- [ ] 2.3 Give the derived state its own table — `ComponentState`, seven values, including the two no collector asserts; verified by the gate.

- [ ] 2.4 Give the summary its own table — `AggregateState`, four values; verified by the gate.

- [ ] 2.5 Answer whether `stale` and `conflict` belong in the collector's table as values it never asserts; verified by the decision being written down either way.

      The design left this open. Comparable tables argue for it; putting a word
      in a table about a vocabulary that does not contain it is what this change
      is against.

## 3. The reasons a collector gives

- [ ] 3.1 Add a section after `### A component row`, where the `reason` field is already named; verified by the gate finding it.

- [ ] 3.2 Explain all twelve values, each read from the mapper that emits it rather than paraphrased from its name; verified by each row naming the condition the mapper applies.

      Ten of the twelve are in the document nowhere today: `baseline`,
      `boot_rebaseline`, `expiry_approaching`, `link_changed`,
      `not_configured`, `owner_unavailable`, `policy_applied`, `probe_failed`,
      `probe_succeeded`, `wake_rebaseline`.

- [ ] 3.3 Record any value whose mapper says something other than its name suggests, as the payload sections did; verified by the row saying what it is emitted for.

## 4. The gate

- [ ] 4.1 Hold the subset case: refuse when a vocabulary's values are satisfied only by rows belonging to a vocabulary that contains it; verified by a fixture of two vocabularies where one is a subset of the other.

- [ ] 4.2 Key a check to a subheading as well as a section; verified by the three tables being held separately.

- [ ] 4.3 Add the seven checks for the vocabularies already explained; verified by each refusing when its value's explanation is removed.

- [ ] 4.4 Add the three checks for the vocabularies gaining a place; verified the same way.

- [ ] 4.5 Say how many vocabularies and values the gate holds in total; verified by the line it prints.

- [ ] 4.6 Prove every one of the ten refuses a removed explanation, and that the six already held still do; verified by one deletion per vocabulary, sixteen in all.

- [ ] 4.7 Prove it fails on the parent commit; verified by its exit status before and after.

## 5. Mutation discipline

- [ ] 5.1 Mutate the subset check and the subheading keying; verified by every survivor closed by a test or recorded with the reason it was left.

      A mutation that does not run is rewritten, not counted. The last run found
      three survivors that were all paths the proofs did not reach, and one of
      them a task record claiming a verification that had not happened — so the
      harness judges by the gate as well as by its own proofs.

## 6. Close

- [ ] 6.1 Confirm all sixteen published vocabularies are held, by reading the code rather than counting the gate's calls; verified by the scan that found the ten.

- [ ] 6.2 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

- [ ] 6.3 Sync the delta into the baseline, validate and archive, with every task that can be ticked beforehand ticked; verified by the drift gate.

- [ ] 6.4 Record what this leaves open.
