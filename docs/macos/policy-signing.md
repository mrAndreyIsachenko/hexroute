# Policy Signing Key

`hexroute-policy sign` reads an Ed25519 seed from a dedicated login-Keychain
generic-password item. The item must require confirmation for every access;
Touch ID is the expected confirmation mechanism on supported Macs. Do not grant
`Allow all applications` access and do not add `security` or `hexroute-policy`
to an always-allow ACL.

The Keychain value is the unpadded base64 encoding of the 32-byte Ed25519 seed.
The corresponding 32-byte public key is stored separately as unpadded base64
and its SHA-256 fingerprint is pinned in the compiled static configuration.
Neither value belongs in public Git. The seed must never be supplied through a
command-line argument, environment variable, log or shell history.

The manual integration gate is intentionally opt-in because it must display and
complete the macOS user-presence prompt:

```sh
HEXROUTE_POLICY_TOUCH_ID_TEST=1 \
HEXROUTE_POLICY_KEYCHAIN_SERVICE='<private service>' \
HEXROUTE_POLICY_KEYCHAIN_ACCOUNT='<private account>' \
HEXROUTE_POLICY_PUBLIC_KEY='<base64 public key>' \
go test -count=1 ./internal/policyapproval -run TestManualKeychainUserPresence
```

Passing the unit tests without this opt-in gate proves only command construction,
seed redaction and key/fingerprint matching. It is not evidence that a live
Keychain item's ACL requires user presence.
