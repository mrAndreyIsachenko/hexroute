#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

project=packaging/macos/policy-profile-host/HexroutePolicyProfile.xcodeproj/project.pbxproj
entitlements=packaging/macos/policy-profile-host/Host/HexroutePolicyProfile.entitlements
embed_script=scripts/macos/embed-policy-signer-binary.sh
build_script=scripts/macos/build-policy-signer-app.sh

for path in "$project" "$entitlements" "$embed_script" "$build_script"; do
  test -s "$path"
done

grep -Fq 'DEVELOPMENT_TEAM = "$(HEXROUTE_POLICY_TEAM_ID)";' "$project"
grep -Fq 'PRODUCT_BUNDLE_IDENTIFIER = "$(HEXROUTE_POLICY_BUNDLE_ID)";' "$project"
grep -Fq 'CODE_SIGN_ENTITLEMENTS = Host/HexroutePolicyProfile.entitlements;' "$project"
grep -Fq 'keychain-access-groups' "$entitlements"
grep -Fq '$(AppIdentifierPrefix)$(PRODUCT_BUNDLE_IDENTIFIER)' "$entitlements"
grep -Fq 'CGO_ENABLED=1' "$embed_script"
grep -Fq 'build -trimpath' "$embed_script"
grep -Fq 'policy signer release build requires a clean Git worktree' "$embed_script"
grep -Fq 'internal/buildinfo.Version=' "$embed_script"
grep -Fq 'internal/buildinfo.Commit=' "$embed_script"
grep -Fq -- '-allowProvisioningUpdates' "$build_script"
grep -Fq 'codesign --verify --deep --strict' "$build_script"
grep -Fq 'profile_authorizes' "$build_script"
grep -Fq '"${permitted%\*}" != *' "$build_script"
grep -Fq 'signed policy compiler identity does not match the clean source revision' "$build_script"

if git ls-files | grep -Eq '\.(provisionprofile|mobileprovision|xcconfig)$'; then
  printf 'tracked private Apple signing artifact detected\n' >&2
  exit 1
fi
if grep -Eq 'DEVELOPMENT_TEAM = [A-Z0-9]{10};' "$project"; then
  printf 'hard-coded Apple Team ID detected\n' >&2
  exit 1
fi

printf 'ok: macOS policy signer profile-host boundary is public and parameterized\n'
