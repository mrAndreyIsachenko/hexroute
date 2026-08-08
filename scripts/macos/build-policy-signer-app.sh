#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
project="$repo_root/packaging/macos/policy-profile-host/HexroutePolicyProfile.xcodeproj"
xcconfig="${1:-$repo_root/.local/policy-signing.xcconfig}"
derived_data="${HEXROUTE_POLICY_DERIVED_DATA:-/private/tmp/hexroute-policy-profile-derived}"
app="$derived_data/Build/Products/Debug/HexroutePolicyProfile.app"
binary="$app/Contents/MacOS/hexroute-policy"
profile="$app/Contents/embedded.provisionprofile"
build_log="$(mktemp /private/tmp/hexroute-policy-xcodebuild.XXXXXX)"
decoded_profile="$(mktemp /private/tmp/hexroute-policy-profile.XXXXXX)"
signed_entitlements="$(mktemp /private/tmp/hexroute-policy-entitlements.XXXXXX)"
trap 'rm -f "$build_log" "$decoded_profile" "$signed_entitlements"' EXIT

if ! git -C "$repo_root" diff --quiet --ignore-submodules -- ||
  ! git -C "$repo_root" diff --cached --quiet --ignore-submodules --; then
  printf 'error: policy signer release build requires a clean Git worktree\n' >&2
  exit 1
fi
expected_commit="$(git -C "$repo_root" rev-parse --verify HEAD)"
expected_version="git.$(git -C "$repo_root" rev-parse --short=12 HEAD)"

if [[ "$(uname -s)" != "Darwin" ]]; then
  printf 'error: macOS is required\n' >&2
  exit 1
fi
if [[ ! -r "$xcconfig" ]]; then
  printf 'error: local signing xcconfig is missing\n' >&2
  exit 1
fi
if ! grep -Eq '^HEXROUTE_POLICY_TEAM_ID[[:space:]]*=[[:space:]]*[^[:space:]]+' "$xcconfig" ||
  ! grep -Eq '^HEXROUTE_POLICY_BUNDLE_ID[[:space:]]*=[[:space:]]*[^[:space:]]+' "$xcconfig"; then
  printf 'error: local signing xcconfig is incomplete\n' >&2
  exit 1
fi

if ! /usr/bin/xcodebuild \
  -project "$project" \
  -scheme HexroutePolicyProfile \
  -configuration Debug \
  -derivedDataPath "$derived_data" \
  -xcconfig "$xcconfig" \
  -allowProvisioningUpdates \
  clean build >"$build_log" 2>&1; then
  tail -n 80 "$build_log" >&2
  exit 1
fi

test -x "$binary"
test -s "$profile"
/usr/bin/codesign --verify --deep --strict "$app"
/usr/bin/codesign -d --entitlements :- "$app" >"$signed_entitlements" 2>/dev/null
/usr/bin/security cms -D -i "$profile" -o "$decoded_profile"

signed_app_id="$(/usr/libexec/PlistBuddy -c 'Print :com.apple.application-identifier' "$signed_entitlements")"
profile_app_id="$(/usr/libexec/PlistBuddy -c 'Print :Entitlements:com.apple.application-identifier' "$decoded_profile")"
signed_group="$(/usr/libexec/PlistBuddy -c 'Print :keychain-access-groups:0' "$signed_entitlements")"
profile_group="$(/usr/libexec/PlistBuddy -c 'Print :Entitlements:keychain-access-groups:0' "$decoded_profile")"

profile_authorizes() {
  local permitted="$1"
  local actual="$2"
  if [[ "$permitted" == "$actual" ]]; then
    return 0
  fi
  if [[ "$permitted" == *'*' && "${permitted%\*}" != *'*' &&
    "$actual" == "${permitted%\*}"* ]]; then
    return 0
  fi
  return 1
}

if [[ -z "$signed_app_id" || -z "$signed_group" || "$signed_app_id" != "$signed_group" ]] ||
  ! profile_authorizes "$profile_app_id" "$signed_app_id" ||
  ! profile_authorizes "$profile_group" "$signed_group"; then
  printf 'error: signed entitlements do not match provisioning profile\n' >&2
  exit 1
fi

"$binary" --check
if [[ "$("$binary" --version)" != "hexroute-policy version=$expected_version commit=$expected_commit" ]]; then
  printf 'error: signed policy compiler identity does not match the clean source revision\n' >&2
  exit 1
fi
printf 'ok: signed Hexroute policy app and provisioning profile verified\n'
printf 'app=%s\n' "$app"
