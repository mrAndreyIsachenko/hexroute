# Local Notifications

`hexroute-userd` owns local macOS notifications because it runs in the login
session. Root `hexrouted` never invokes a GUI process.

The notification policy accepts only validated typed incidents. Immediate
delivery is limited to critical incidents, exhausted recovery budgets and
security-validation failures. A single degraded Telegram provider remains
non-actionable while another provider is healthy. Resolutions during the
configured night window are marked for a later morning digest instead of
creating an immediate notification.

Notification title and body text come from a closed template allowlist. Live
hostnames, addresses, profile identifiers, incident identifiers, command
output and credentials are never interpolated into AppleScript.
`/usr/bin/osascript` is executed directly without a shell.

Delivery is best effort and bounded:

- successful delivery is deduplicated by incident generation and status;
- a failed attempt is retried no more than once per minute;
- in-memory deduplication state is capped at 256 entries;
- notification failure records only a generic degraded result and never stops
  the local control loop.

A local delivery does not acknowledge or delete a pending external alert.
That state remains pending for the independent Telegram delivery workflow.
The current user daemon feeds Pritunl `SAFE_MODE` transitions into this policy.
Other local and cloud incidents use the same typed service as their incident
producers are enabled.
