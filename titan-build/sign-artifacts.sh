#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
task_artifact_dir="${TITAN_ARTIFACT_DIR:-$PWD/dist}"
[[ "${GITHUB_ACTIONS:-}" == true && -n "${RUNNER_TEMP:-}" && -n "${TITAN_SIGNING_KEY:-}" ]]
umask 077
task_key_dir=$(mktemp -d "$RUNNER_TEMP/titan-signing.XXXXXX")
trap 'rm -rf "$task_key_dir"' EXIT
printf '%s\n' "$TITAN_SIGNING_KEY" > "$task_key_dir/private.pem"
openssl pkey -in "$task_key_dir/private.pem" -pubout -out "$task_key_dir/public.pem"
cmp "$task_key_dir/public.pem" .titan/release-public.pem
cp .titan/release-public.pem "$task_artifact_dir/release-public.pem"
(
  cd "$task_artifact_dir"
  find . -maxdepth 1 -type f ! -name SHA256SUMS ! -name SHA256SUMS.sig -printf '%f\n' \
    | LC_ALL=C sort | while IFS= read -r task_file; do sha256sum "$task_file"; done > SHA256SUMS
  sha256sum --check SHA256SUMS
)
openssl pkeyutl -sign -rawin -inkey "$task_key_dir/private.pem" -in "$task_artifact_dir/SHA256SUMS" -out "$task_artifact_dir/SHA256SUMS.sig"
openssl pkeyutl -verify -rawin -pubin -inkey .titan/release-public.pem -in "$task_artifact_dir/SHA256SUMS" -sigfile "$task_artifact_dir/SHA256SUMS.sig"
