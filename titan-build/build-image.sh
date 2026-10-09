#!/usr/bin/env bash
# Build the full TitanOS baseline with its own branding and signed update feed.
set -euo pipefail

TASK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 "${TASK_ROOT}/titan-build/verify-source.py" "${TASK_ROOT}"
python3 "${TASK_ROOT}/titan-build/configure-branding.py" "${TASK_ROOT}"
python3 "${TASK_ROOT}/titan-build/configure-updater.py" "${TASK_ROOT}"
RELEASE_METADATA="${TASK_ROOT}/.titan/release.json"
RELEASE_VERSION="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "${RELEASE_METADATA}")"
OS_VERSION="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["osVersion"])' "${RELEASE_METADATA}")"
UPSTREAM_COMMIT="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["upstreamCommit"])' "${RELEASE_METADATA}")"
BUILD_COMMIT="$(git -C "${TASK_ROOT}" rev-parse HEAD)"
ARTIFACT_DIR="${TITAN_ARTIFACT_DIR:-${TASK_ROOT}/dist}"
ASSET_BASE="titan-${RELEASE_VERSION}"
if [[ ! "${RELEASE_VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ || "${OS_VERSION}" != "${RELEASE_VERSION}" || ! "${UPSTREAM_COMMIT}" =~ ^[0-9a-f]{40}$ ]]; then
    echo "Unsupported baseline release version: ${RELEASE_VERSION}" >&2
    exit 1
fi
if [[ "$(uname -m)" != "x86_64" ]]; then
    echo "Build TitanOS on a native AMD64 Linux runner." >&2
    exit 1
fi
for task_command in docker npm python3 xz; do
    command -v "${task_command}" >/dev/null || { echo "Missing build tool: ${task_command}" >&2; exit 1; }
done
docker buildx version
mkdir -p "${ARTIFACT_DIR}"
ARTIFACT_DIR="$(cd "${ARTIFACT_DIR}" && pwd)"
python3 - "${TASK_ROOT}" <<'PY'
import shutil, sys
available = shutil.disk_usage(sys.argv[1]).free
required = 35 * 1024**3
if available < required:
    raise SystemExit(f"Need at least 35 GiB free for the OS build; available {available / 1024**3:.1f} GiB")
PY

# Preserve attribution both in the installed OS and beside every image.
NOTICE_DIR="${TASK_ROOT}/packages/os/overlay/usr/share/doc/titan"
mkdir -p "${NOTICE_DIR}"
cp "${TASK_ROOT}/LICENSE.md" "${NOTICE_DIR}/LICENSE.md"
cp "${TASK_ROOT}/UPSTREAM.md" "${NOTICE_DIR}/UPSTREAM.md"
cp "${NOTICE_DIR}/LICENSE.md" "${ARTIFACT_DIR}/LICENSE.md"
cp "${NOTICE_DIR}/UPSTREAM.md" "${ARTIFACT_DIR}/UPSTREAM.md"
cp "${RELEASE_METADATA}" "${ARTIFACT_DIR}/release.json"

cd "${TASK_ROOT}/packages/os"
# Rugix keys imported roots by path. Clear project state so a new release
# cannot reuse stale root files; Docker Buildx's layer cache remains available.
if [[ -d "${TASK_ROOT}/packages/os/rugix/.rugix" ]]; then
    sudo -n rm -rf "${TASK_ROOT}/packages/os/rugix/.rugix"
fi
# Bake the exact Titan build version into the Rugix OS metadata.
# Build the native AMD64 Rugix layout for fresh TitanOS installations.
SKIP_CACHE_EXPORT=true npm run build:amd64:rugix -- "${OS_VERSION}"

RAW_IMAGE="${ARTIFACT_DIR}/${ASSET_BASE}.img"
mv build/titanos-amd64.img "${RAW_IMAGE}"
mv build/titanos-amd64.update "${ARTIFACT_DIR}/${ASSET_BASE}.update"
# Keep the compact provisioning template; first boot uses the target disk capacity.
VERIFY_ARGUMENTS=("${RAW_IMAGE}" --manifest "${ARTIFACT_DIR}/image-verification.json" --release-metadata "${RELEASE_METADATA}")
if [[ "${RUN_IMAGE_SMOKE:-1}" == "1" ]]; then
    VERIFY_ARGUMENTS+=(--smoke --vm-script "${TASK_ROOT}/packages/os/vm.sh" --boot-log "${ARTIFACT_DIR}/boot-smoke.log" --timeout "${IMAGE_SMOKE_TIMEOUT:-1200}")
fi
python3 "${TASK_ROOT}/titan-build/verify-image.py" "${VERIFY_ARGUMENTS[@]}"
if [[ "${RUN_IMAGE_SMOKE:-1}" == "1" ]]; then
    # Exercise the real NetworkManager bridge migration on a fresh guest before
    # compression/signing. A failed LAN change must block release publication.
    npm --prefix "${TASK_ROOT}/packages/titand" ci
    # Custom QCOW2 must pass the real product API on the OS being published.
    TITAN_VM_IMAGE="${RAW_IMAGE}" npm --prefix "${TASK_ROOT}/packages/titand" run test -- --pool=forks --minWorkers=1 --maxWorkers=1 source/modules/machines/custom-qcow2.vm.test.ts --reporter=verbose --reporter=json --outputFile="${ARTIFACT_DIR}/qcow2-smoke.json"
    python3 - "${ARTIFACT_DIR}/image-verification.json" "${ARTIFACT_DIR}/qcow2-smoke.json" <<'PYQCOW'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
report = json.loads(pathlib.Path(sys.argv[2]).read_text())
if report.get('success') is not True or report.get('numPassedTests') != 3 or any(report.get(key) != 0 for key in ('numFailedTests', 'numPendingTests', 'numTodoTests')):
    raise SystemExit('Custom QCOW2 import must pass all three real VM tests')
data = json.loads(path.read_text())
data['customQcow2Smoke'] = {'status': 'passed', 'passedTests': 3, 'checks': ['authenticated import and independent disk', 'host reboot and source preservation', 'host backing file rejection']}
path.write_text(json.dumps(data, indent=2) + '\n')
PYQCOW
    TITAN_VM_IMAGE="${RAW_IMAGE}" npm --prefix "${TASK_ROOT}/packages/titand" run test -- --pool=forks --minWorkers=1 --maxWorkers=1 source/modules/machines/automatic-bridge.vm.test.ts --reporter=verbose --reporter=json --outputFile="${ARTIFACT_DIR}/bridge-smoke.json"
    python3 - "${ARTIFACT_DIR}/image-verification.json" "${ARTIFACT_DIR}/bridge-smoke.json" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
report = json.loads(pathlib.Path(sys.argv[2]).read_text())
if report.get("success") is not True or report.get("numFailedTests") != 0 or report.get("numPendingTests") != 0 or report.get("numPassedTests", 0) < 12:
    raise SystemExit("The real guest bridge tests did not all pass")
verification = json.loads(path.read_text())
verification["bridgeNetworkSmoke"] = {"status": "passed", "passedTests": report["numPassedTests"], "report": pathlib.Path(sys.argv[2]).name}
path.write_text(json.dumps(verification, indent=2) + "\n")
PY
    # Build private, separately signed older-version fixtures outside dist.
    # The signing key is removed from the real VM test process environment.
    TASK_RECOVERY_ROOT=$(mktemp -d "${RUNNER_TEMP:-/tmp}/titan-recovery.XXXXXX")
    trap 'rm -rf "${TASK_RECOVERY_ROOT}"' EXIT
    python3 "${TASK_ROOT}/titan-build/prepare-recovery-fixture.py" --root "${TASK_ROOT}" --artifacts "${ARTIFACT_DIR}" --output "${TASK_RECOVERY_ROOT}/fixture"
    env -u TITAN_SIGNING_KEY TITAN_RECOVERY_FIXTURE="${TASK_RECOVERY_ROOT}/fixture" TITAN_RECOVERY_EVIDENCE="${ARTIFACT_DIR}/recovery-evidence.json" npm --prefix "${TASK_ROOT}/packages/titand" run test -- --pool=forks --minWorkers=1 --maxWorkers=1 source/modules/system/recovery.vm.test.ts --reporter=verbose --reporter=json --outputFile="${ARTIFACT_DIR}/recovery-smoke.json"
    python3 - "${TASK_ROOT}" "${ARTIFACT_DIR}" "${RELEASE_VERSION}" "${BUILD_COMMIT}" <<'PYTHON'
import json, pathlib, sys
sys.path.insert(0, str(pathlib.Path(sys.argv[1]) / 'titan-build'))
from recovery_gate import validate
path = pathlib.Path(sys.argv[2]) / 'image-verification.json'
verification = json.loads(path.read_text())
verification['systemRecoverySmoke'] = validate(path.parent, sys.argv[3], sys.argv[4])
path.write_text(json.dumps(verification, indent=2) + '\n')
PYTHON
    rm -rf "${TASK_RECOVERY_ROOT}"
    trap - EXIT
fi
# Stream the compact disk template, then verify the compressed download.
# The recovery fixture already compressed the candidate's exact bytes.
if [[ ! -f "${RAW_IMAGE}.xz" ]]; then
    xz --threads=2 --memlimit-compress=2GiB --stdout "${RAW_IMAGE}" > "${RAW_IMAGE}.xz"
fi
xz --test "${RAW_IMAGE}.xz"
rm "${RAW_IMAGE}"

python3 - "${ARTIFACT_DIR}" "${RELEASE_METADATA}" "${BUILD_COMMIT}" <<'PY'
import datetime, hashlib, json, pathlib, sys
directory = pathlib.Path(sys.argv[1])
release = json.loads(pathlib.Path(sys.argv[2]).read_text())
verification = json.loads((directory / "image-verification.json").read_text())
assets = sorted(p for p in directory.iterdir() if p.is_file() and p.name.startswith("titan-") and p.suffix in {".xz", ".update"})
expected = {f'titan-{release["version"]}.img.xz', f'titan-{release["version"]}.update'}
if len(assets) != 2 or {p.name for p in assets} != expected:
    raise SystemExit("Release must contain the exact TitanOS image and update bundle")
def digest(path):
    hasher = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            hasher.update(block)
    return hasher.hexdigest()
for asset in assets:
    if not 0 < asset.stat().st_size < 2 * 1024**3:
        raise SystemExit(f"GitHub release asset must be smaller than 2 GiB: {asset.name}")
manifest = {
    "schemaVersion": 1,
    "releaseVersion": release["version"],
    "upstreamVersion": release["upstreamTag"],
    "osVersion": release["osVersion"],
    "stage": release["stage"],
    "systemCompatibility": release["systemCompatibility"],
    "upstreamRepository": release["upstreamRepository"],
    "upstreamCommit": release["upstreamCommit"],
    "buildCommit": sys.argv[3],
    "builtAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "architecture": "amd64",
    "firmware": "UEFI",
    "updateFormat": "rugix",
    "updateProvider": release["updateProvider"],
    "ownTitanUpdateChannel": True,
    "releaseEligible": verification["structuralCheck"] == "passed" and verification["uefiHttpSmoke"]["status"] == "passed" and verification.get("bridgeNetworkSmoke", {}).get("status") == "passed" and verification.get("systemRecoverySmoke", {}).get("status") == "passed",
    "imageVerification": verification,
    "assets": [{"name": p.name, "sizeBytes": p.stat().st_size, "sha256": digest(p)} for p in assets],
}
(directory / "build-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
checksum_files = sorted(p for p in directory.iterdir() if p.is_file() and p.name not in {"SHA256SUMS", "SHA256SUMS.sig"})
(directory / "SHA256SUMS").write_text("".join(f"{digest(p)}  {p.name}\n" for p in checksum_files))
print(f"Verified release assets: {', '.join(p.name for p in assets)}")
PY
