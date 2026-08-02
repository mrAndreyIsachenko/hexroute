# Policy Signer Profile Host

This minimal macOS application target asks Xcode to create the development
provisioning profile used to package the local `hexroute-policy` signer. It has
no networking or runtime behavior.

Keep the real Apple Team ID, bundle identifier, provisioning profile and build
products outside Git. The local build uses an ignored xcconfig:

```text
HEXROUTE_POLICY_TEAM_ID = <private team id>
HEXROUTE_POLICY_BUNDLE_ID = <private unique bundle id>
```

Build the real Go signer with Xcode automatic signing:

```sh
scripts/macos/build-policy-signer-app.sh .local/policy-signing.xcconfig
```

Using `/private/tmp` avoids File Provider metadata that invalidates macOS code
signatures when a repository is stored in a synchronized `Documents` folder.

The Xcode build phase replaces the inert host executable with the cgo-enabled
`hexroute-policy` binary before Xcode embeds the profile and signs the app. The
wrapper verifies that the signed entitlements match the profile and that the
embedded CLI starts successfully.
