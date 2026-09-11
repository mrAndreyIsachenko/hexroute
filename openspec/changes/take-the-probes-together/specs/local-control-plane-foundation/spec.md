## ADDED Requirements

### Requirement: An observation cycle waits once for what it can wait for together

A runtime SHALL NOT hold its operator socket for the sum of observations that do
not depend on one another. Where observations are independent, the cycle SHALL
wait for them together and take no longer than the slowest.

The socket is answered between cycles, so what a cycle spends is what a caller
waits. Endpoint probes are the case that matters: each waits on a network that
answers or does not, they say nothing to each other, and taking them in turn
made the cycle cost their sum.

Taking them together SHALL NOT change what is observed. The cycle SHALL fold the
results in configuration order and reach the same summary it would have reached
in sequence, including which failure is recorded when more than one fails.

#### Scenario: Several endpoints are probed

- **WHEN** a cycle probes more than one endpoint
- **THEN** it waits for them together and costs about as long as the slowest rather than as long as all of them

#### Scenario: The order of answers does not decide the summary

- **WHEN** probes complete in an order other than the configured one
- **THEN** the summary is the one the configured order produces, whichever answer arrived first

#### Scenario: More than one probe fails

- **WHEN** several probes fail in the same cycle
- **THEN** the failure the cycle records is the one the configured order would have left, not the one that happened to finish last
