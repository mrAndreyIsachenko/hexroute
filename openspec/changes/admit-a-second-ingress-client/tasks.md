# Tasks

## 1. Record The Clients

- [ ] 1.1 Add a public document recording the client population the way the fleet document records hosts: what each client is for, that each holds its own identity, and how one is removed. It names no credential, address or transport parameter.
- [ ] 1.2 Gate the document the way the fleet document is gated, so it cannot come to name a live address or a transport parameter later.

## 2. Recover A Published Version

- [ ] 2.1 Add an operator step that verifies a published version against the pinned operator public key and the content digest, and emits the exact bytes unparsed. Assert that a version failing either check emits nothing and names the check that failed.
- [ ] 2.2 Put it in the binary that already signs, and assert it stays offline: no network and no database, so the ability to read a version is not the ability to publish one.
- [ ] 2.3 Assert the emitted content cannot be written into this repository.

## 3. Keep Delivery Blind

- [ ] 3.1 Assert that the version format, its verification and the agent decode nothing of a version's content beyond the digest that binds it. A version carrying a configuration for a runtime this repository knows nothing about is delivered, verified, applied and returned from exactly as any other.

## 4. Correct The Record

- [x] 4.1 Record roadmap item 6 as taken apart rather than done: MTProto, MTG, the Nginx split and the second provider are ruled out with their reasons, so the next reader does not derive them again.
- [x] 4.2 Correct the roadmap's account of why the provider-B host cannot take a second port. Its module accepts up to eight rules and allows only ports 22 and 443, requiring one global 443 rule and permitting 22 only from `/32` networks. The conclusion held; the reason written down did not.
- [x] 4.3 Record what the grill found and this change does not fix (done when the change was planned, not after it was built: the roadmap must not go on stating a reason the project has decided against, and a finding recorded only in a grill transcript is a finding lost): the external monitor's single alert contact, the signed heartbeat's transport health being two TCP dials, and the Telegram SLO calculation that has no producer and now describes a service that will not be built.

## 5. Verify

- [ ] 5.1 Run `make check` and resolve every failure.
- [ ] 5.2 Run `openspec validate admit-a-second-ingress-client --strict` and keep proposal, design, specs and tasks consistent with what was built.
- [ ] 5.3 Sync the delta into the baseline specs and archive the change.
