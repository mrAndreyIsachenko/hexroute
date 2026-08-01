## 1. Module Contract

- [x] 1.1 Add the `lightsail-ingress` module with a constrained AWS provider contract and validated provider-neutral inputs.
- [x] 1.2 Implement one instance, static IPv4, attachment and authoritative public-port resource without provider or backend configuration.
- [x] 1.3 Expose only non-secret resource identities, computed addresses and normalized firewall metadata.

## 2. Regression Evidence

- [x] 2.1 Extend public-boundary tests to reject live identities, secret-bearing inputs, public SSH, IPv6, broad protocols and unrelated resource types.
- [x] 2.2 Extend the synthetic mock-provider fixture with valid Lightsail composition and negative firewall validation cases.
- [x] 2.3 Run Terraform formatting, contract, mock-provider, state and secret checks.

## 3. Documentation And Coordination

- [x] 3.1 Update public Terraform documentation without claiming production readiness or automatic failover.
- [x] 3.2 Run strict repository OpenSpec validation and the full public repository check.
- [ ] 3.3 Mark private parent task 4.1 complete only after the public commit is pushed and record rollback as removal before private adoption.
