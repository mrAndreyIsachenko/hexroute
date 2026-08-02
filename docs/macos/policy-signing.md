# Policy Signing Key

`hexroute-policy sign` reads an Ed25519 seed from a dedicated macOS Data
Protection Keychain generic-password item. The item requires system user
presence for every access; Touch ID is the expected confirmation mechanism on
supported Macs. The implementation does not use the legacy file-based Keychain
ACL model.

The Keychain value is the unpadded base64 encoding of the 32-byte Ed25519 seed.
The corresponding 32-byte public key is stored separately as unpadded base64
and its SHA-256 fingerprint is pinned in the compiled static configuration.
Neither value belongs in public Git. The seed must never be supplied through a
command-line argument, environment variable, log or shell history.

The live signer must be an app-like, Developer-signed macOS bundle with an
embedded provisioning profile authorizing its application identifier. An
unsigned binary, `go run`, ad-hoc signature or standalone test binary is
expected to fail closed with `keychain_entitlement_required`; do not replace
this boundary with a legacy file-based Keychain item.

Create an ignored `.local/policy-signing.xcconfig` with private
`HEXROUTE_POLICY_TEAM_ID` and unique `HEXROUTE_POLICY_BUNDLE_ID` values, then
build and validate the signer app:

```sh
scripts/macos/build-policy-signer-app.sh .local/policy-signing.xcconfig
```

The command reports the local `.app` path. Use its
`Contents/MacOS/hexroute-policy` executable for every command below.

Using the installed signed binary, provision a new item and private local
public-key metadata without exposing the seed in argv:

```sh
hexroute-policy provision-key \
  --keychain-service '<private service>' \
  --keychain-account '<private account>' \
  --out "$HOME/Library/Application Support/Hexroute/policy-signer"
```

Provisioning refuses to replace an existing item. On macOS it creates a local,
non-synchronizable Data Protection Keychain item with
`SecAccessControlUserPresence` and
`kSecAttrAccessibleWhenPasscodeSetThisDeviceOnly`. Each read therefore requires
system user presence, normally Touch ID, rather than an always-allow application
grant. Provisioning fails closed on unsupported platforms or when cgo is
disabled.

Confirm the installed signer, pinned public key and Touch ID path with one
fixed-challenge signature:

```sh
hexroute-policy verify-key \
  --keychain-service '<private service>' \
  --keychain-account '<private account>' \
  --public-key "$HOME/Library/Application Support/Hexroute/policy-signer/public-key"
```

The same signed executable must perform provisioning, verification and policy
signing because Data Protection Keychain access groups are derived from the
host executable's validated entitlements. The repository contains no
provisioning profile, Team ID, application identifier value, signer fingerprint
or generated public key.

Passing unit tests proves only native bridge behavior, seed redaction and
key/fingerprint matching. Only a successful `verify-key` run from the installed
signed executable, with the macOS user-presence prompt completed, is accepted
as live integration evidence.

The current bounded integration record is in
[`docs/testing/policy-signer-live-evidence.md`](../testing/policy-signer-live-evidence.md).

Apple platform references:

- [TN3137: On Mac keychains](https://developer.apple.com/documentation/Technotes/tn3137-on-mac-keychains)
- [`kSecUseDataProtectionKeychain`](https://developer.apple.com/documentation/security/ksecusedataprotectionkeychain)
- [`SecAccessControlCreateFlags.userPresence`](https://developer.apple.com/documentation/security/secaccesscontrolcreateflags/userpresence)
