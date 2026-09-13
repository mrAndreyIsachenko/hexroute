# Tasks

## 1. The capability exists and is bounded

- [x] 1.1 Add the capability to the model, and to the root domain's envelope only.
- [x] 1.2 Test that a generation granting it to the user domain is refused at compile time.

## 2. The runtime asks

- [x] 2.1 Add the handler method that answers whether it would authorize.
- [x] 2.2 Ask on every cycle that decides to act, and on no others.

## 3. The answer is recorded

- [x] 3.1 Extend the decision record with the authorization outcome and its closed reasons.
- [x] 3.2 Test that a refusal under the active generation is recorded with its reason.

## 4. Gates and evidence

- [x] 4.1 Mutate the envelope, the asking and the recording; confirm the named tests fail.

      Three fail a named test: allow the user domain to own the tunnel, ask on a
      cycle that decided nothing, and let a refusal call itself authorized.

      The first is proved through the compiler rather than against the envelope.
      The last capability added here was defined, permitted, asked for by both
      runtimes and written into a runbook without ever being compiled — every
      part proven and the path between them not — so this one is driven from a
      source an operator would write, through composition, to the evaluator the
      runtime asks.
- [x] 4.2 `make check` green.
- [ ] 4.3 Install, and read back a decision that says it would not have been allowed.

## 5. Close

- [ ] 5.1 Sync the deltas into the baselines, validate, archive.
