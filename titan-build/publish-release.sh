#!/usr/bin/env bash
# Publish only a signed, boot-tested stable profile; public tags stay immutable.
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TASK_ARTIFACT_DIR="${TITAN_ARTIFACT_DIR:-${TASK_ROOT}/dist}"
cd "${TASK_ROOT}"
openssl pkeyutl -verify -rawin -pubin -inkey .titan/release-public.pem \
  -in "${TASK_ARTIFACT_DIR}/SHA256SUMS" -sigfile "${TASK_ARTIFACT_DIR}/SHA256SUMS.sig"
(cd "${TASK_ARTIFACT_DIR}"; sha256sum --check SHA256SUMS)
python3 - "${TASK_ARTIFACT_DIR}" <<'PY'
import json, pathlib, sys
d = pathlib.Path(sys.argv[1])
m = json.loads((d/'build-manifest.json').read_text())
r = json.loads((d/'release.json').read_text())
assert m['releaseEligible'] and m['imageVerification']['uefiHttpSmoke']['status'] == 'passed'
assert m['stage'] == r['stage'] == 'stable'
assert m['releaseVersion'] == m['osVersion'] == r['version'] == r['osVersion']
assert m['imageVerification']['uefiHttpSmoke']['installedRelease']['version'] == r['version']
assert (d/'SHA256SUMS.sig').is_file()
PY
task_version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "${TASK_ARTIFACT_DIR}/release.json")
task_bridge=$(python3 -c 'import json,sys; print("true" if json.load(open(sys.argv[1])).get("legacyUpdateBridgeTo") else "false")' "${TASK_ARTIFACT_DIR}/release.json")
task_tag="v${task_version}"
task_source=$(git rev-parse HEAD)
if gh release view "$task_tag" --json isDraft --jq .isDraft > "$RUNNER_TEMP/titan-release-exists" 2>/dev/null; then
  if [[ "$(cat "$RUNNER_TEMP/titan-release-exists")" != true ]]; then
    echo 'An already published release is immutable; increment .titan/release.json.' >&2
    exit 1
  fi
else
  gh release create "$task_tag" --draft --prerelease=false --target "$task_source" \
    --title "TitanOS 2.0.0 — ${task_version}" --notes-file titan-build/RELEASE.md
fi
if [[ "$task_bridge" == true ]]; then
  # Existing updater metadata keeps its actual legacy build identifier. Users
  # get the compact canonical IMG from v2.0.0, not an extra bridge disk image.
  task_assets=()
  for task_asset in "${TASK_ARTIFACT_DIR}"/*; do
    [[ "$task_asset" == *.img.xz ]] || task_assets+=("$task_asset")
  done
  gh release upload "$task_tag" "${task_assets[@]}" --clobber
  gh release edit "$task_tag" --draft=false --prerelease=false --latest=false
else
  gh release upload "$task_tag" "${TASK_ARTIFACT_DIR}"/* --clobber
  gh release edit "$task_tag" --draft=false --prerelease=false --latest=true
fi
