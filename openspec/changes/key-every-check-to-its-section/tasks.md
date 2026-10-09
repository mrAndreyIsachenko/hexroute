# Tasks

## 1. The ground

- [x] 1.1 Audit every value of every remaining check against the section it appears in; verified by the counts per vocabulary.

      Audited 2026-10-09. Every value of all seven vocabularies is explained in
      its own section today, so **nothing is currently undocumented** — the gate
      passes for the right reason by luck rather than by construction.

      Six of the seven have values that also appear elsewhere: all eight
      component names in their payload sections; the component states
      `conflict`, `degraded`, `ready`, `stale`, `unknown`; `unauthorized`; the
      classifications `conflict`, `missing`, `stale`, `unknown`; and the diff
      reason `none`, which appears in Authorization and six payload sections.
      Only `proposal class` is confined to one section.

- [x] 1.2 Demonstrate that a removed explanation goes unnoticed, rather than arguing it from the overlap; verified by the gate's exit status with the row deleted.

      The row explaining the component state `degraded` — "the owner asserted
      partial function" — was deleted from `### Component states` and the gate
      exited **0**. The word survives in the `managed_transports` payload
      section, where it counts degraded transports and says nothing about what a
      degraded component is. The reference was restored and the diff is empty.

      That is the defect, and it is a loss the gate cannot see rather than a
      falsehood it currently tells.

## 2. The reader

- [ ] 2.1 Give the payload helper a second entry point: a vocabulary, a section and a document; verified by a test over a fixture section.

- [ ] 2.2 Refuse when the section is absent, rather than reading the whole document; verified by a test over a document with no such heading.

- [ ] 2.3 Refuse a value the section explains and the code no longer declares, naming it; verified by a test over a fixture with a row for a value not in the vocabulary.

- [ ] 2.4 Name rows the section carries that are not values of its vocabulary, rather than treating them as stale; verified by a test over a section holding both.

      `### A component row` explains record fields rather than states, so a
      section legitimately carrying other rows must not be read as describing a
      narrowed vocabulary.

- [ ] 2.5 Say how many sections and values were held; verified by the line it prints.

## 3. The seven checks

- [ ] 3.1 Key the component check to the section that owns the component names; verified by the gate refusing when a name's row is removed from it.

- [ ] 3.2 Key the component state check to `### Component states`; verified by the `degraded` deletion from 1.2 now being refused.

- [ ] 3.3 Key the authorization check to `## Authorization`.

- [ ] 3.4 Key the classification and diff reason checks to `## The diff`.

- [ ] 3.5 Key the proposal class check to `## The proposals`.

- [ ] 3.6 Key the source check to the section that owns the declared sources.

- [ ] 3.7 Record any value that turns out to be explained outside its own section, and move its explanation rather than widening the check; verified by the gate passing without a widened check.

## 4. The gate holds

- [ ] 4.1 Prove each of the seven refuses a removed explanation; verified by one deletion per vocabulary, each expected to fail.

      Seven deletions, not one. The `degraded` case is the one already
      demonstrated; the other six are the ones this change is for.

- [ ] 4.2 Prove it fails on the parent commit; verified by its exit status before and after.

- [ ] 4.3 Prove a fully explained reference still passes, so the refusals are about what is missing; verified by the gate's exit status on the document as it stands.

## 5. Mutation discipline

- [ ] 5.1 Mutate the section bounding and both refusals; verified by every survivor closed by a test or recorded with the reason it was left.

      A mutation that does not compile — or, for a shell gate, does not run — is
      rewritten, not counted.

## 6. Close

- [ ] 6.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

- [ ] 6.2 Confirm the rename reached the baseline as a rename and not as a second requirement; verified by reading the baseline after archiving.

      The delta renames the requirement added last change and replaces its
      content. A `MODIFIED` block replaces the whole requirement, and the
      validator already refused a first attempt that silently dropped three
      scenarios the current spec holds.

- [ ] 6.3 Sync the delta into the baseline, validate and archive, with every task above ticked first; verified by the drift gate.

- [ ] 6.4 Record what this leaves open.
