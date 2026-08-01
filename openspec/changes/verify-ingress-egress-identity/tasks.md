## 1. Request Contract

- [x] 1.1 Add strict optional expected-body SHA-256 validation to authenticated requests.

## 2. Bounded Verification

- [x] 2.1 Compare the bounded SOCKS response body without exposing body or digest data.
- [x] 2.2 Preserve status-only behavior when no digest is supplied.

## 3. Regression Evidence

- [x] 3.1 Add tests for match, mismatch, oversized response, invalid digest, redaction and cleanup.
- [x] 3.2 Update public probe documentation and run strict repository checks.

## 4. Rollout

- [x] 4.1 Record the exact accepted public commit for private canary adoption and retain rollback to the preceding commit.
