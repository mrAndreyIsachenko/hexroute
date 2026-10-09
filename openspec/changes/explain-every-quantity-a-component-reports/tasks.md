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

- [ ] 1.2 Read each payload's mapper, so an explanation says what the quantity counts rather than what its name suggests; verified by each section naming the mapper's own condition.

## 2. The record

- [ ] 2.1 Rename the scoped-routes section to the component's own spelling, so the heading can be derived from the constant; verified by the gate finding it.

- [ ] 2.2 Explain `physical_network`: `link_class`, `link_up`, `has_carrier`.

- [ ] 2.3 Explain `default_path`: `path_class`, `gateway_present`.

- [ ] 2.4 Explain `dns`: `resolver_class`, `responding`, `scoped_domains`, `failing_domains`.

      This component has no collector — an earlier change recorded that no
      mapper can emit one — so its payload is explained as what it would report,
      and the section says the component is not yet produced.

- [ ] 2.5 Explain `managed_transports`: `configured`, `ready`, `degraded`, with the subject each counts here rather than the one the same words carry elsewhere.

- [ ] 2.6 Explain `relay_ingress`: `configured`, `reachable`, `reserve`, `selected_class`.

- [ ] 2.7 Explain `user_access`: `profile_class`, `connected`, `authenticated`.

- [ ] 2.8 Explain `session_expiry`: `expiry_class`, `sessions`.

## 3. The gate

- [ ] 3.1 Read the payload types and their `json` tags from the code, with no list of payloads in the gate; verified by a test that a payload added to the code is required without the gate being edited.

- [ ] 3.2 Look for each field only between its component's heading and the next; verified by a test that a row under another component does not satisfy it.

- [ ] 3.3 Refuse an explained field no payload carries; verified by a test over a row for a field that does not exist.

- [ ] 3.4 Refuse rather than pass when it finds no payloads to check; verified by a test that points it at a file declaring none.

- [ ] 3.5 Say how many payloads and fields were held; verified by the line it prints.

- [ ] 3.6 Prove it fails on the parent commit; verified by its exit status before and after.

## 4. Close

- [ ] 4.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

- [ ] 4.2 Sync the delta into the baseline, validate and archive; verified by the drift gate.

- [ ] 4.3 Record what this leaves open, including whether the gate's other checks share the flat-match weakness.
