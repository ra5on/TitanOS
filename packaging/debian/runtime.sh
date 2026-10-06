#!/bin/bash
set -euo pipefail
export LC_ALL=C.UTF-8
component=all
json=false
dry_run=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        --component) component="${2:?Component required}"; shift 2 ;;
        --json) json=true; shift ;;
        --dry-run) dry_run=true; shift ;;
        *) exit 2 ;;
    esac
done
[[ "$component" =~ ^(all|docker|vms)$ ]] || exit 2
if $dry_run; then echo 'Debian: vorinstallierte Komponenten prüfen; keine Paketinstallation, keine Datenlaufwerke verändern.'; exit 0; fi
[[ "$EUID" == 0 && -d /run/systemd/system ]] || exit 1
/usr/bin/python3 - <<'PY'
import json, platform
from pathlib import Path
info=json.loads(Path('/usr/share/titan/image-info.json').read_text())
osinfo=platform.freedesktop_os_release()
assert osinfo['ID']=='debian' and osinfo['VERSION_ID']=='13'
assert (info['platform'],info['format'])in (('debian-preview','titan-debian-preview-v1'),('debian-rauc','titan-debian-ab-v1'))
PY
source /usr/share/titan/component-functions.sh

# The runtime oneshot has a 30-second deadline. Bound every daemon request and
# the fresh offline proof together, rather than letting socket activation or a
# failed service retry consume the entire management startup deadline.
task_runtime_deadline=$((SECONDS + 24))
docker_programs_verified=false
vm_programs_verified=false
docker_start_failed=false
vm_start_failed=false

runtime_command() {
    local task_limit="$1" task_remaining=$((task_runtime_deadline - SECONDS))
    shift
    (( task_remaining > 0 )) || return 124
    if (( task_limit > task_remaining )); then task_limit="$task_remaining"; fi
    /usr/bin/timeout --signal=TERM --kill-after=1s "${task_limit}s" "$@"
}

runtime_vm_support_sockets_ready() {
    local task_socket
    # A multi-unit is-active succeeds when ANY unit is active. Verify every
    # supporting listener separately, including after a guarded daemon failure.
    # The primary libvirt socket is separately proved after a daemon denial.
    for task_socket in virtlogd.socket virtlockd.socket; do
        runtime_command 1 systemctl is-active --quiet "$task_socket" || return 1
    done
}

install_apps() {
    command -v docker >/dev/null 2>&1 || { printf '%s\n' 'Docker fehlt im Titan-Systemimage.' >&2; return 1; }
    runtime_command 2 docker compose version || { printf '%s\n' 'Docker Compose fehlt oder antwortet nicht.' >&2; return 1; }
    docker_programs_verified=true
    runtime_command 2 systemctl enable docker.service || return 1
    if ! runtime_command 5 systemctl start docker.service; then
        docker_start_failed=true
        return 1
    fi
    runtime_command 2 docker info --format '{{.ServerVersion}}'
}

install_vm_components() {
    [[ "$(uname -m)" == x86_64 ]] || return 1
    verify_vm_programs || return 1
    vm_programs_verified=true
    runtime_command 3 /bin/bash -c 'source /usr/share/titan/component-functions.sh; prepare_vm_runtime' || return 1
    runtime_command 3 systemctl enable libvirtd.socket virtlogd.socket virtlockd.socket || return 1
    # A blocked main daemon may also close its primary/companion listeners.
    # Only the fixed main-process guard proof can excuse the primary socket;
    # virtlogd and virtlockd must remain active. An enable failure stays fatal.
    runtime_command 3 systemctl start libvirtd.socket virtlogd.socket virtlockd.socket || true
    runtime_vm_support_sockets_ready || { printf '%s\n' 'Ein erforderlicher VM-Socket ist nicht aktiv.' >&2; return 1; }
    # Explicitly attempt the guarded daemon: socket availability alone is not
    # evidence that the main-process guard ran or that the backend can be used.
    if ! runtime_command 5 systemctl start libvirtd.service; then
        vm_start_failed=true
        return 1
    fi
    runtime_command 1 systemctl is-active --quiet libvirtd.socket || return 1
    runtime_command 5 /bin/bash -c 'source /usr/share/titan/component-functions.sh; activate_vm_services'
}

runtime_memory_blocked() {
    # Only a failed actual daemon start with all selected programs present can
    # qualify. Missing packages, Compose/noVNC, sockets or network setup remain
    # errors even if a separate, old RAM report happens to exist.
    if [[ "$component" == all || "$component" == docker ]]; then
        $docker_programs_verified && $docker_start_failed || return 1
    fi
    if [[ "$component" == all || "$component" == vms ]]; then
        $vm_programs_verified && $vm_start_failed || return 1
    fi
    # The proof covers the shared Docker/libvirt boot budget, so even a
    # selected-component repair must not hide missing programs on the other
    # side while accepting the restricted management state.
    if ! $docker_programs_verified; then
        command -v docker >/dev/null 2>&1 || return 1
        runtime_command 2 docker compose version || return 1
    fi
    if ! $vm_programs_verified; then
        [[ "$(uname -m)" == x86_64 ]] && verify_vm_programs || return 1
    fi
    # Wait for systemd's real failed end state after the wrapper's main exit.
    # RestartPreventExitStatus=78 prevents the guard restart loop. Activating,
    # a timeout or an exhausted shared deadline is never a valid RAM block.
    local task_failed_deadline=$((SECONDS + 6))
    while ! { runtime_command 1 systemctl is-failed --quiet docker.service &&
              runtime_command 1 systemctl is-failed --quiet libvirtd.service; }; do
        (( SECONDS < task_failed_deadline && SECONDS < task_runtime_deadline )) || return 1
        runtime_command 1 sleep 0.25 || return 1
    done
    runtime_vm_support_sockets_ready || return 1
    runtime_command 15 /usr/bin/python3 -I - <<'PY'
import sys
sys.path.insert(0, '/usr/lib/titan')
from titan.debian_updates import verified_boot_memory_block, verified_guard_socket
# Prove both exact main-process exits and their fresh component denial markers
# before checking primary listeners. A stopped socket is only valid with its
# own exact guard evidence, never because another daemon or cached JSON failed.
if verified_boot_memory_block() is None:
    sys.exit(1)
sys.exit(0 if all(verified_guard_socket(unit) for unit in
                 ('docker.socket', 'libvirtd.socket')) else 1)
PY
}

failed=0
docker_state=skip
vm_state=skip
if [[ "$component" == all || "$component" == docker ]]; then
    if install_apps >&2; then docker_state=ok; else docker_state=failed; failed=1; fi
fi
if [[ "$component" == all || "$component" == vms ]]; then
    if install_vm_components >&2; then vm_state=ok; else vm_state=failed; failed=1; fi
fi
if [[ "$failed" == 1 ]] && runtime_memory_blocked >&2; then
    failed=0
    [[ "$docker_state" != failed ]] || docker_state=ram_blocked
    [[ "$vm_state" != failed ]] || vm_state=ram_blocked
    printf '%s\n' 'RAM-Schutz bestätigt: Docker und virtuelle Maschinen bleiben angehalten. Weboberfläche und Dateimanager starten im eingeschränkten Betrieb weiter.' >&2
fi
if $json; then
    python3 - "$docker_state" "$vm_state" <<'PY'
import json,os,sys
values={k:{'ok':v=='ok','state':v} for k,v in zip(('docker','vms'),sys.argv[1:]) if v!='skip'}
warnings=['KVM fehlt: Virtualisierung im BIOS oder Hypervisor aktivieren.'] if sys.argv[2]=='ok' and not os.path.exists('/dev/kvm') else []
if 'ram_blocked' in sys.argv[1:]:
    warnings.append('RAM-Schutz: Docker und virtuelle Maschinen sind angehalten; RAM-Zuweisung erhöhen. Weboberfläche und Dateimanager bleiben verfügbar.')
print(json.dumps({'ok':all(v['ok'] for v in values.values()),'components':values,'warnings':warnings}))
PY
fi
exit "$failed"
