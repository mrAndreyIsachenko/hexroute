## 1. Structured Bootstrap

- [x] 1.1 Add nullable exact-version, bounded-HTTPS and SHA-256 artifact inputs without an arbitrary user-data or secret input.
- [x] 1.2 Add deterministic cloud-init and verified installer templates for XRay and the ingress observer.
- [x] 1.3 Add hardened XRay and observer systemd templates with runtime-file activation gates and least privilege.
- [x] 1.4 Pass rendered cloud-init to the Lightsail instance and expose only its SHA-256 digest and non-secret artifact versions.

## 2. Regression Evidence

- [x] 2.1 Add mock rendering tests for valid pins, absent bootstrap and rejection of floating versions, unsafe URLs and malformed digests.
- [x] 2.2 Add static tests for checksum-before-install, non-root units, capability boundaries, runtime-file gates and secret-bearing input rejection.
- [x] 2.3 Run Terraform formatting, contract, mock-provider, state and secret checks.

## 3. Documentation And Coordination

- [x] 3.1 Document bootstrap/process/state ownership without claiming activation or production readiness.
- [x] 3.2 Run strict OpenSpec validation and the full public repository gate.
- [x] 3.3 Push the public revision, pin it in private policy, mark parent task 4.2 complete and record pre-adoption rollback.

## 4. Observer Artifact Completion

- [x] 4.1 Implement the strict loopback-only signed ingress observer with bounded XRay and outbound dependency checks.
- [x] 4.2 Extend the heartbeat probe with a validated literal-loopback SOCKS retrieval path and keep non-loopback endpoints HTTPS-only.
- [x] 4.3 Add a deterministic static Linux AMD64 archive builder and double-build reproducibility, archive-content and secret-redaction tests.
- [x] 4.4 Update runtime documentation, run the full public gate and publish an immutable observer release with its SHA-256 digest.
- [ ] 4.5 Pin the released observer and official XRay artifacts in private policy before any SSH or runtime apply.
