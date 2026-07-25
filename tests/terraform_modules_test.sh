#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$repo_root/terraform/test-fixtures/modules"
workdir="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-terraform-test.XXXXXX")"
plugin_cache="${TF_PLUGIN_CACHE_DIR:-${TMPDIR:-/tmp}/hexroute-terraform-plugin-cache}"
trap 'rm -rf "$workdir"' EXIT

mkdir -p "$plugin_cache"
cp -R "$repo_root/terraform" "$workdir/terraform"

export TF_DATA_DIR="$workdir/terraform-data"
export TF_PLUGIN_CACHE_DIR="$plugin_cache"
export TF_IN_AUTOMATION=1

terraform -chdir="$workdir/terraform/test-fixtures/modules" \
  init -backend=false -input=false -test-directory=tests
terraform -chdir="$workdir/terraform/test-fixtures/modules" validate
terraform -chdir="$workdir/terraform/test-fixtures/modules" \
  test -test-directory=tests

printf 'terraform synthetic composition passed\n'
