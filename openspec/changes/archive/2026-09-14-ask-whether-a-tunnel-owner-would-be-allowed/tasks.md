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

      This test proved the record and not the question. Its authority discarded
      the generation and the digest and returned a policy-shaped refusal, so it
      passed while the runtime sent generation zero and an empty digest — a
      request the evaluator rejects as malformed before reading any policy.
      Found 2026-09-14 by reading the machine, not by any gate. The fake now
      keeps both arguments, and the handler is also asked for real in
      `internal/policycontrol/tunnel_authorization_test.go`: malformed without
      them, `selector_mismatch` under a generation granting nothing,
      `authorized` under one that grants the capability.

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

      Six more on 2026-09-14, after the malformed question was found, all six
      killed: sending generation zero again, sending an empty digest again,
      asking with no control state, a digest that ignores the generation, a
      digest that ignores the causes, and the handler asking about the wrong
      capability.
- [x] 4.2 `make check` green.
- [x] 4.3 Install, and read back a decision that says it would not have been allowed.

      First read 2026-09-14: 2072 tunnel decisions held, 683 with an answer,
      every recent one `invalid_request`. That satisfied this task's wording and
      not its purpose. The runtime asked `AuthorizeTunnelOwnership("tunnel", 0,
      "")`, and the evaluator requires a non-zero control-state generation and a
      SHA-256 plan digest, so it refused the question before consulting policy.
      A generation granting the capability would have left every record exactly
      so, which the code's own comment denied.

      The runtime now asks under the operator snapshot's generation with a
      digest of the decision, and asks nothing before that snapshot has a
      generation. Not yet closed: the daemon has to be installed from this
      branch and a decision read back whose reason is the policy's —
      `selector_mismatch` under generation 4 — rather than `invalid_request`.

      Read back 2026-09-14 after installing from the branch at 09:20:31Z. Ten
      decisions: the first, at 09:22:24Z, carries no answer — the cycle before
      the operator snapshot had a generation, which asks nothing by design — and
      the other nine all read `{"allowed": false, "reason": "selector_mismatch"}`.
      The question reaches policy, and policy answers it.

      The reader first called that single unanswered decision proof that the
      daemon does not ask. One decision cannot tell an old daemon from a first
      cycle; the reader now says so instead of reaching a verdict.

## 5. Close

- [x] 5.1 Sync the deltas into the baselines, validate, archive.
