# macOS Policy Signer Live Evidence

Date: 2026-08-02

The macOS user-presence policy signer boundary was exercised on an Apple
silicon Mac with a Personal Team Apple Development identity:

1. Xcode automatic signing issued and embedded a development provisioning
   profile for a private local application identifier.
2. The app signature passed strict verification.
3. The signed application identifier and Keychain access group were authorized
   by the embedded profile. Exact identifiers were not recorded.
4. The signed cgo-enabled `hexroute-policy` executable provisioned a new,
   non-replacing Data Protection Keychain Ed25519 seed protected by
   `SecAccessControlUserPresence`.
5. The same executable completed `verify-key` after the macOS user-presence
   challenge and verified a fixed-challenge signature against the separately
   stored public key.
6. The unsigned negative control failed closed with
   `keychain_entitlement_required` and created neither the Keychain item nor
   output metadata.

The Team ID, application identifier, provisioning profile, Keychain account,
public key and signer fingerprint remain outside Git. The prior experimental
Keychain item was not modified or removed. No networking, routing, Twilight or
AdGuard state was changed during this test.
