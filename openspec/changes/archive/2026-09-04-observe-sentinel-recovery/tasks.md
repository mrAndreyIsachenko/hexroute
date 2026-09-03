# Tasks

## 1. Run The Planner

- [x] 1.1 Build the recovery planner and an observe-only controller inside the sentinel's cycle, with a nil restarter, so acting is impossible rather than declined.
- [x] 1.2 Pass the cycle's decision to the controller each observation and carry the resulting plan on the summary.
- [x] 1.3 Prove an observing controller cannot act: with no restarter, an evidence-ready decision produces a plan and no attempt.
- [x] 1.4 Prove a planner error does not stop the cycle. The sentinel's job is to keep watching, and a planner that refused an input must not take the watching with it.

## 2. Record What It Would Have Done

- [x] 2.1 Replace the per-cycle evidence warning with the plan: the phase, the selected action, and that nothing was done.
- [x] 2.2 Record on transition rather than per cycle, so the same condition holding for a day is three lines and not a thousand.
- [x] 2.3 Record the attempt bound being reached as its own event, distinct from another cycle of evidence.
- [x] 2.4 Prove the recorded phase follows the planner rather than the raw evidence, including the case where evidence is incomplete.

## 3. Verification And Documentation

- [x] 3.1 Document what an observing sentinel records, what each phase means, and what the bound being reached says about a future cutover.
- [x] 3.2 Remove `NewRecoveryPlanner` and `NewRecoveryController` from the unbuilt-capability list, and restate why `NewMacOSRootRestarter` remains.
- [x] 3.3 Run focused unit and race tests for the sentinel, and `make check`.
- [x] 3.4 Run `openspec validate observe-sentinel-recovery --strict` and keep proposal, design, specs and tasks synchronized with what was built.
- [x] 3.5 Sync the delta requirement into the baseline spec only after an observing sentinel has recorded a plan on a real host.
- [x] 3.6 Give the sentinel a way to run. It had no launchd job and no installer, so the binary a SHALL requirement describes had never been deployed and could not be — which is what task 3.5 was actually waiting on.
