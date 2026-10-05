#!/usr/bin/env bash
# Fail before an OS build when the configured secret cannot sign trusted updates.
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -z "${TITAN_SIGNING_KEY:-}" || -z "${RUNNER_TEMP:-}" ]]; then
    echo 'The existing TITAN_SIGNING_KEY secret and a disposable runner directory are required.' >&2
    exit 1
fi
umask 077
task_key_dir=$(mktemp -d "$RUNNER_TEMP/titan-key-check.XXXXXX")
trap 'rm -rf "$task_key_dir"' EXIT
printf '%s\n' "$TITAN_SIGNING_KEY" > "$task_key_dir/private.pem"
if ! openssl pkey -in "$task_key_dir/private.pem" -passin pass: -pubout -out "$task_key_dir/public.pem" 2>/dev/null; then
    echo 'TITAN_SIGNING_KEY is not a valid private signing key.' >&2
    exit 1
fi
if ! cmp --silent "$task_key_dir/public.pem" "$TASK_ROOT/.titan/release-public.pem"; then
    echo 'TITAN_SIGNING_KEY does not match the existing Titan public key. Use the same signing key as before.' >&2
    exit 1
fi
echo 'The signing key matches the pinned Titan public key.'
