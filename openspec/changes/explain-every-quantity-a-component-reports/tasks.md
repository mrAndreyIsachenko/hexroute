# Tasks

## 1. The ground

- [x] 1.1 Read how many payloads and fields are explained, per payload rather than per word; verified by the counts and by the false passes the per-word reading produced.

      Measured 2026-10-09. Eight payload types in
      `internal/connectivity/payload.go`, 25 fields, and one type with every
      field in a table row: `ScopedRoutesPayload`, documented last week by the
      change that added a field to it. `RelaysPayload` has one of four.

      Per word the answer was different and wrong. A first reading said 0
      undocumented of 31 — a broken shell loop, because zsh does not split a
      variable into words. A second said 20 of 31 by matching a backtick-quoted
      word anywhere. The third, per payload type and per table row, found
      `TransportsPayload` "explained" because `configured` matched a row about
      routes and `ready` and `degraded` matched rows about component states.

      Three false passes, each found only by reading the document for the
      specific word. That is the finding the change is built on, not a detail of
      how it was measured.

- [x] 1.2 Read each payload's mapper, so an explanation says what the quantity counts rather than what its name suggests; verified by each section naming the mapper's own condition.

      Six quantities turned out to say something other than their names, and
      the sections say so rather than paraphrasing the field:

      - `physical_network.has_carrier` is set from the **gateway being valid**,
        not from a link-layer carrier.
      - `physical_network.link_class` reads `wired` for any link that is up. The
        mapper's own comment says the observer cannot tell wired from wireless;
        the value still says `wired`, and `wireless`, `cellular` and `virtual`
        are in the vocabulary with nothing emitting them.
      - `default_path.gateway_present` means a gateway was observed under
        `tunneled` and is set to true with the class under `direct`, so the two
        classes do not make the same claim with it.
      - `managed_transports.configured` is the literal **1** the root cycle
        passes, and `degraded` is **never set** by any mapper.
      - `relay_ingress.reserve` and `selected_class` are the call site's
        constants — zero and `primary` — not observations.
      - `session_expiry.sessions` is one or zero and has never been anything
        else, and `expiry_class` never reaches `expiring` or `expired`.

      Found while reading: the doc comment above `MapScopedRoutes` still carried
      two lines of the comment the previous change replaced, describing the
      behaviour that change removed. Deleted here.

## 2. The record

- [x] 2.1 Rename the scoped-routes section to the component's own spelling, so the heading can be derived from the constant; verified by the gate finding it.

      `### The scoped routes payload` became ``### The `scoped_routes` payload``.

- [x] 2.2 Explain `physical_network`: `link_class`, `link_up`, `has_carrier`.

- [x] 2.3 Explain `default_path`: `path_class`, `gateway_present`.

- [x] 2.4 Explain `dns`: `resolver_class`, `responding`, `scoped_domains`, `failing_domains`.

      The section says plainly that nothing produces this component, that a test
      asserts no mapper can, and that the fields describe what the payload is
      shaped to carry rather than anything a reader will meet today.

- [x] 2.5 Explain `managed_transports`: `configured`, `ready`, `degraded`, with the subject each counts here rather than the one the same words carry elsewhere.

      And the section names the collision itself: these three words are also a
      route count, a relay count and two component states.

- [x] 2.6 Explain `relay_ingress`: `configured`, `reachable`, `reserve`, `selected_class` — two of the four being the call site's constants.

- [x] 2.7 Explain `user_access`: `profile_class`, `connected`, `authenticated` — the last being inferred rather than checked.

- [x] 2.8 Explain `session_expiry`: `expiry_class`, `sessions`.

## 3. The gate

- [x] 3.1 Read the payload types and their `json` tags from the code, with no list of payloads in the gate; verified by the gate's own fixture declaring a payload it has never heard of and being required to explain it.

      Better than planned. The `Payload` struct's own json tags **are** the
      component names and they name each payload type, so the gate derives the
      whole mapping from one declaration and holds no list at all — not of
      payloads, not of components, not of fields. Its fixture declares an
      `ExamplePayload` under a component called `example`, and the gate demands
      an explanation for it.

- [x] 3.2 Look for each field only between its component's heading and the next; verified by a fixture whose rows sit under another component's heading and are refused.

- [x] 3.3 Refuse an explained field no payload carries; verified by a fixture with a row for `removed`.

- [x] 3.4 Refuse rather than pass when it finds no payloads to check; verified by a fixture declaring none.

      Two refusals, not one: no `Payload` struct at all, and a `Payload` struct
      naming nothing. A fully explained fixture is also required to pass, so the
      refusals are about what is missing rather than the gate refusing
      everything.

- [x] 3.5 Say how many payloads and fields were held; verified by the line it prints: `8 payloads, 25 fields`.

- [x] 3.6 Prove it fails on the parent commit; verified by its exit status before and after.

      With the reference's new sections stashed it exits **1** and names every
      component with no section; with them, **0**.

## 4. Close

- [ ] 4.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

- [ ] 4.2 Sync the delta into the baseline, validate and archive; verified by the drift gate.

- [ ] 4.3 Record what this leaves open, including whether the gate's other checks share the flat-match weakness.
