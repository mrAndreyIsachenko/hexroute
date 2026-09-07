# Tasks

## 1. Draw The Line Between The Kinds

- [ ] 1.1 Stop treating a cross-domain overlap of action selectors as an ownership violation, and keep treating credentials, routes and endpoints as one. Assert the distinction directly: two domains claiming one credential is still rejected, two domains authorized for one capability and target is not.
- [ ] 1.2 Assert that differing effects across domains compile. Root allowing while user denies is what a partial rollback leaves behind, and refusing it would refuse the step at the moment it is needed.
- [ ] 1.3 Assert nothing else moved: same-domain action rules with differing effects still conflict, and overlapping routes, endpoints and credentials still do.

## 2. Tie The Envelope To The Compiler

- [ ] 2.1 Derive the cases from the envelope rather than naming them. Every capability the envelope permits in both domains must compile in both — so the test covers the capability added next year, not the one that failed this week.
- [ ] 2.2 Assert the converse still holds: a capability or target outside the envelope is still refused, and the envelope remains the authority the compiler defers to.

## 3. Cross The Boundary That Hid This

- [ ] 3.1 Compile a candidate that grants a capability, then evaluate its payload under an active generation and lease, and assert the act is authorized in each domain the candidate granted it to. The capability was defined, permitted, asked for by both runtimes and documented, and never compiled; what was missing is a test that goes from the compiler's output to the evaluator's input.
- [ ] 3.2 Assert the negative through the same path: without a lease, or under a generation that does not grant it, the compiled payload authorizes nothing. Deny-by-default is what makes a grant meaningful.

## 4. Verify

- [ ] 4.1 Run `make check` and resolve every failure.
- [ ] 4.2 For each new property, restore the defect it guards and confirm the named test fails. Reinstating the unconditional cross-domain rule must fail the two-sided test; removing the envelope-derived case must fail the property test.
- [ ] 4.3 Run `openspec validate permit-two-sided-action-capabilities --strict` and keep proposal, design, specs and tasks consistent with what was built.
- [ ] 4.4 Sync the delta into the baseline specs and archive. This change closes on its tests: compiling, signing and activating generation 4 is the cutover's own evidence, in the change this unblocks.
