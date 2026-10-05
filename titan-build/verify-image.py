#!/usr/bin/env python3
"""Check the actual disk image and, optionally, its UEFI HTTP startup."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import struct
import subprocess
import tempfile
import time
import urllib.request
import uuid
import zlib


SECTOR_SIZE = 512
EFI_TYPE = "c12a7328-f81f-11d2-ba4b-00a0c93ec93b"
MINIMUM_BOOT_DISK_SIZE = 32 * 1024**3
SYSTEM_SLOT_SIZE = 10 * 1024**3


def require(condition, message):
    if not condition:
        raise ValueError(message)


def read_at(stream, offset, length):
    stream.seek(offset)
    value = stream.read(length)
    require(len(value) == length, f"Truncated image at byte {offset}")
    return value


def read_header(stream, lba):
    sector = read_at(stream, lba * SECTOR_SIZE, SECTOR_SIZE)
    require(sector[:8] == b"EFI PART", "GPT signature is missing")
    revision, size, checksum = struct.unpack_from("<III", sector, 8)
    require(revision == 0x10000 and 92 <= size <= SECTOR_SIZE, "Invalid GPT header")
    checked = bytearray(sector[:size])
    checked[16:20] = bytes(4)
    require(zlib.crc32(checked) == checksum, "GPT header CRC does not match")
    current, backup, first, last = struct.unpack_from("<QQQQ", sector, 24)
    entries_lba, count, entry_size, entries_crc = struct.unpack_from("<QIII", sector, 72)
    require(current == lba and 0 < count <= 4096 and 128 <= entry_size <= 4096, "Invalid GPT entry bounds")
    entries = read_at(stream, entries_lba * SECTOR_SIZE, count * entry_size)
    require(zlib.crc32(entries) == entries_crc, "GPT partition array CRC does not match")
    return {"current": current, "backup": backup, "first": first, "last": last, "entrySize": entry_size, "entries": entries, "diskGuid": sector[56:72]}


def verify_first_boot_capacity(image_size, system_partition, boot_disk_size=MINIMUM_BOOT_DISK_SIZE):
    require(boot_disk_size >= MINIMUM_BOOT_DISK_SIZE, "Target boot disk needs at least 32 GiB for first-boot A/B and data partitions")
    require(0 < image_size <= boot_disk_size, "Target boot disk is smaller than the compact provisioning image")
    require(system_partition["sizeBytes"] <= SYSTEM_SLOT_SIZE, "Provisioning system filesystem exceeds the 10 GiB update slot")
    alignment = 2048
    system_b_start = ((system_partition["firstLba"] + SYSTEM_SLOT_SIZE // SECTOR_SIZE + alignment - 1) // alignment) * alignment
    data_start = system_b_start + SYSTEM_SLOT_SIZE // SECTOR_SIZE
    # Reserve the backup GPT and at least 1 GiB of actual data capacity.
    require(data_start * SECTOR_SIZE + 1024**3 + 33 * SECTOR_SIZE <= boot_disk_size, "First-boot A/B layout leaves too little space for data")


def verify_efi_loader(stream, partition):
    start = partition["firstLba"] * SECTOR_SIZE
    size = partition["sizeBytes"]
    boot = read_at(stream, start, SECTOR_SIZE)
    require(boot[510:512] == b"\x55\xaa", "EFI FAT boot-sector signature is missing")
    sector_size = struct.unpack_from("<H", boot, 11)[0]
    sectors_per_cluster = boot[13]
    reserved = struct.unpack_from("<H", boot, 14)[0]
    fats = boot[16]
    root_entries = struct.unpack_from("<H", boot, 17)[0]
    total_sectors = struct.unpack_from("<H", boot, 19)[0] or struct.unpack_from("<I", boot, 32)[0]
    fat_size_16 = struct.unpack_from("<H", boot, 22)[0]
    fat_size = fat_size_16 or struct.unpack_from("<I", boot, 36)[0]
    require(sector_size == SECTOR_SIZE and sectors_per_cluster in {1, 2, 4, 8, 16, 32, 64, 128}, "Unsupported EFI FAT geometry")
    require(reserved > 0 and fats in {1, 2} and fat_size > 0, "Invalid EFI FAT geometry")
    require(0 < total_sectors <= size // sector_size, "EFI FAT filesystem extends outside its partition")
    root_sectors = (root_entries * 32 + sector_size - 1) // sector_size
    root_sector = reserved + fats * fat_size
    data_sector = root_sector + root_sectors
    require(data_sector < total_sectors, "EFI FAT data region is empty")
    cluster_count = (total_sectors - data_sector) // sectors_per_cluster
    fat_bits = 12 if cluster_count < 4085 else 16 if cluster_count < 65525 else 32
    require(fat_bits in {16, 32}, "Expected FAT16 or FAT32 EFI filesystem")
    require((fat_bits == 32 and root_entries == 0 and fat_size_16 == 0) or (fat_bits == 16 and root_entries > 0 and fat_size_16 > 0), "EFI FAT type and BPB geometry disagree")
    # Rugix Bakery v0.9.3 uses mkfs.vfat without -F32, so its 256 MiB ESP
    # is FAT16 even though the project layout calls the filesystem Fat32.
    # FAT16 stores its root directory before the data area; FAT32 uses a
    # cluster chain. Interpreting the FAT16 label as a root cluster loses EFI.
    root_cluster = struct.unpack_from("<I", boot, 44)[0] if fat_bits == 32 else None
    cluster_bytes = sectors_per_cluster * sector_size
    max_cluster = cluster_count + 1
    fat_entry_bytes = fat_bits // 8
    require(fat_size * sector_size >= (cluster_count + 2) * fat_entry_bytes, "EFI FAT table is too small for its data region")
    fat_base = start + reserved * sector_size
    if fat_bits == 32:
        flags = struct.unpack_from("<H", boot, 40)[0]
        if flags & 0x80:
            active_fat = flags & 0x0F
            require(active_fat < fats, "EFI FAT32 active table index is invalid")
            fat_base += active_fat * fat_size * sector_size
    end_of_chain = 0x0FFFFFF8 if fat_bits == 32 else 0xFFF8

    def cluster_offset(cluster):
        require(2 <= cluster <= max_cluster, "EFI cluster lies outside its partition")
        return start + (data_sector + (cluster - 2) * sectors_per_cluster) * sector_size

    def directory_chunks(cluster):
        if cluster is None:
            yield read_at(stream, start + root_sector * sector_size, root_entries * 32)
            return
        visited = set()
        while cluster < end_of_chain:
            require(cluster not in visited and len(visited) < 4096, "EFI FAT directory chain loops")
            visited.add(cluster)
            yield read_at(stream, cluster_offset(cluster), cluster_bytes)
            entry = read_at(stream, fat_base + cluster * fat_entry_bytes, fat_entry_bytes)
            cluster = struct.unpack("<I" if fat_bits == 32 else "<H", entry)[0]
            if fat_bits == 32:
                cluster &= 0x0FFFFFFF

    def directory(cluster):
        for data in directory_chunks(cluster):
            for offset in range(0, len(data), 32):
                item = data[offset:offset + 32]
                if item[0] == 0:
                    return
                if item[0] == 0xE5 or item[11] == 0x0F or item[11] & 0x08:
                    continue
                base = item[:8].decode("ascii", errors="replace").rstrip()
                extension = item[8:11].decode("ascii", errors="replace").rstrip()
                name = base + ("." + extension if extension else "")
                child = struct.unpack_from("<H", item, 26)[0]
                if fat_bits == 32:
                    child |= struct.unpack_from("<H", item, 20)[0] << 16
                yield name.upper(), child, bool(item[11] & 0x10), struct.unpack_from("<I", item, 28)[0]

    cluster = root_cluster
    for name in ("EFI", "BOOT", "BOOTX64.EFI"):
        found = next((item for item in directory(cluster) if item[0] == name), None)
        require(found is not None, f"Required UEFI fallback loader path component missing: {name} (FAT{fat_bits})")
        _, cluster, is_directory, file_size = found
        require(is_directory == (name != "BOOTX64.EFI"), f"Incorrect UEFI loader path component: {name}")
    require(file_size > 0 and read_at(stream, cluster_offset(cluster), 2) == b"MZ", "UEFI fallback loader is not a PE executable")
    return {"path": "EFI/BOOT/BOOTX64.EFI", "sizeBytes": file_size, "filesystem": f"FAT{fat_bits}"}


def inspect_image(image):
    image_size = image.stat().st_size
    require(image_size > 2 * SECTOR_SIZE and image_size % SECTOR_SIZE == 0, "Invalid raw image size")
    with image.open("rb") as stream:
        mbr = read_at(stream, 0, SECTOR_SIZE)
        require(mbr[510:512] == b"\x55\xaa" and any(mbr[446 + i * 16 + 4] == 0xEE for i in range(4)), "Protective GPT MBR is missing")
        primary = read_header(stream, 1)
        require(primary["backup"] == image_size // SECTOR_SIZE - 1, "Backup GPT is not at the end of the image")
        backup = read_header(stream, primary["backup"])
        require(backup["backup"] == 1 and backup["entries"] == primary["entries"], "Primary and backup GPT disagree")
        partitions = []
        entries = primary["entries"]
        for offset in range(0, len(entries), primary["entrySize"]):
            entry = entries[offset:offset + primary["entrySize"]]
            if entry[:16] == bytes(16):
                continue
            first, last = struct.unpack_from("<QQ", entry, 32)
            require(primary["first"] <= first <= last <= primary["last"], "Partition extends outside GPT usable space")
            partitions.append({"number": offset // primary["entrySize"] + 1, "type": str(uuid.UUID(bytes_le=entry[:16])), "name": entry[56:128].decode("utf-16-le").split("\0", 1)[0], "firstLba": first, "lastLba": last, "sizeBytes": (last - first + 1) * SECTOR_SIZE})
        require(len(partitions) == 4, "Expected the original four-partition provisioning image")
        ordered = sorted(partitions, key=lambda p: p["firstLba"])
        require(all(a["lastLba"] < b["firstLba"] for a, b in zip(ordered, ordered[1:])), "GPT partitions overlap")
        require(partitions[0]["type"] == EFI_TYPE, "First partition is not an EFI System Partition")
        require(partitions[1]["sizeBytes"] == 512 * 1024**2, "Unexpected boot partition size")
        require(500 * 1000**2 <= partitions[2]["sizeBytes"] <= 512 * 1024**2, "Unexpected inactive boot reservation")
        verify_first_boot_capacity(image_size, partitions[3])
        for partition in (partitions[1], partitions[3]):
            magic = read_at(stream, partition["firstLba"] * SECTOR_SIZE + 1024 + 56, 2)
            require(magic == b"\x53\xef", f"Partition {partition['number']} is not an Ext4 filesystem")
        loader = verify_efi_loader(stream, partitions[0])
    hasher = hashlib.sha256()
    with image.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            hasher.update(block)
    return {"rawImage": image.name, "sizeBytes": image_size, "sha256": hasher.hexdigest(), "structuralCheck": "passed", "partitions": partitions, "efiLoader": loader, "firstBootLayout": {"minimumDiskGiB": 32, "systemSlotsGiB": [10, 10], "dataFilesystem": "ext4", "dataUsesRemainingDisk": True}, "uefiHttpSmoke": {"status": "not-run"}}


def available_port():
    with socket.socket() as connection:
        connection.bind(("127.0.0.1", 0))
        return connection.getsockname()[1]


class BrandingMismatch(ValueError):
    """A ready backend belongs to a different release or product."""


def check_system_version(body, expected_version, expected_name):
    require(isinstance(body, dict) and "error" not in body, "system.version tRPC returned an error")
    result = body.get("result")
    require(isinstance(result, dict), "system.version tRPC has no result")
    version_info = result.get("data")
    require(isinstance(version_info, dict), "system.version tRPC has no version object")
    actual_version, actual_name = version_info.get("version"), version_info.get("name")
    require(isinstance(actual_version, str) and isinstance(actual_name, str), "system.version response has no version/name strings")
    if (actual_version, actual_name) != (expected_version, expected_name):
        raise BrandingMismatch(
            f"Wrong installed release: expected {expected_name!r} version {expected_version!r}; "
            f"received {actual_name!r} version {actual_version!r}"
        )
    return {"version": actual_version, "name": actual_name}


def smoke_boot(image, vm_script, boot_log, timeout, release_metadata=None):
    if release_metadata is None:
        release_metadata = next(
            (parent / ".titan/release.json" for parent in vm_script.resolve().parents if (parent / ".titan/release.json").is_file()),
            None,
        )
    require(release_metadata is not None, "Smoke test needs the expected Titan release metadata")
    metadata = json.loads(release_metadata.read_text())
    expected_version, expected_name = metadata.get("osVersion"), metadata.get("versionName")
    require(isinstance(expected_version, str) and bool(expected_version), "Release metadata has no osVersion")
    require(isinstance(expected_name, str) and bool(expected_name), "Release metadata has no versionName")
    for tool in ("qemu-system-x86_64", "qemu-img", "jq"):
        require(shutil.which(tool) is not None, f"Missing smoke-test tool: {tool}")
    require(Path("/usr/share/OVMF/OVMF_CODE_4M.fd").exists(), "Install OVMF before running the UEFI smoke test")
    temporary = Path(tempfile.mkdtemp(prefix="titan-image-smoke-"))
    script = temporary / "vm.sh"
    script_text = vm_script.read_text()
    acceleration = "kvm"
    # The original Linux VM helper assumes KVM. Use an isolated helper copy
    # with software emulation on runners that do not expose /dev/kvm.
    if not Path("/dev/kvm").exists():
        acceleration = "tcg"
        script_text = script_text.replace('accel_args="-enable-kvm"', 'accel_args="-accel tcg,thread=multi"')
        script_text = script_text.replace('machine_args="-machine accel=kvm,type=q35"', 'machine_args="-machine type=q35"')
        script_text = script_text.replace('cpu_args="-cpu host"', 'cpu_args="-cpu max"')
        script_text = script_text.replace('qemu_sudo="sudo"', 'qemu_sudo=""')
    script.write_text(script_text)
    script.chmod(0o755)
    port = available_port()
    ssh_port = available_port()
    while ssh_port == port:
        ssh_port = available_port()
    boot_log.parent.mkdir(parents=True, exist_ok=True)
    process = None
    started = time.monotonic()
    try:
        with boot_log.open("wb") as log:
            # Test the documented target size in a private overlay. Flashing
            # uses the target's capacity and leaves the compact RAW unchanged.
            process = subprocess.Popen([str(script), "boot", str(image.resolve()), "--arch", "amd64", "--device", "umbrel-home", "--memory", "4096", "--cores", "2", "--disk-size", str(MINIMUM_BOOT_DISK_SIZE), "--http-port", str(port), "--ssh-port", str(ssh_port)], env={**os.environ, "VM_STATE_DIR": str(temporary / "state")}, stdout=log, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL, start_new_session=True)
            last_error = "No HTTP response"
            while time.monotonic() - started < timeout:
                require(process.poll() is None, f"QEMU exited before startup; see {boot_log}")
                try:
                    with urllib.request.urlopen(f"http://127.0.0.1:{port}/", timeout=5) as response:
                        html = response.read(2 * 1024 * 1024).decode("utf-8", errors="replace")
                        require(response.status == 200 and "<html" in html.lower(), "Desktop HTML is not ready")
                    with urllib.request.urlopen(f"http://127.0.0.1:{port}/trpc/user.exists", timeout=5) as response:
                        body = json.load(response)
                        require(response.status == 200 and "result" in body and "error" not in body, "Backend tRPC startup check failed")
                    with urllib.request.urlopen(f"http://127.0.0.1:{port}/trpc/system.version", timeout=5) as response:
                        require(response.status == 200, "system.version endpoint is not ready")
                        installed_release = check_system_version(json.load(response), expected_version, expected_name)
                    return {"status": "passed", "acceleration": acceleration, "memoryMiB": 4096, "bootDiskSizeBytes": MINIMUM_BOOT_DISK_SIZE, "rawImageSizeBytes": image.stat().st_size, "elapsedSeconds": round(time.monotonic() - started, 1), "checks": ["UEFI disk boot on 32 GiB target disk", "desktop HTML over guest network", "user.exists tRPC over guest network", "exact Titan version and product name over guest network"], "installedRelease": installed_release, "log": boot_log.name}
                except BrandingMismatch:
                    # A healthy backend with wrong branding will not become
                    # the expected release by waiting another twenty minutes.
                    raise
                except (OSError, ValueError) as error:
                    last_error = str(error)
                time.sleep(3)
            raise ValueError(f"UEFI startup timed out after {timeout}s: {last_error}; see {boot_log}")
    finally:
        if process is not None and process.poll() is None:
            # QEMU may have been launched as root by sudo. Its private QMP
            # socket provides reliable cleanup even if sudo changed sessions.
            state_name = str((temporary / "state").resolve())
            socket_name = f"/tmp/umbrel-vm-{hashlib.sha256(state_name.encode()).hexdigest()[:16]}.qmp"
            quit_code = "import socket,sys; s=socket.socket(socket.AF_UNIX); s.settimeout(5); s.connect(sys.argv[1]); s.recv(4096); s.sendall(b'{\"execute\":\"qmp_capabilities\"}\\n'); s.recv(4096); s.sendall(b'{\"execute\":\"quit\"}\\n'); s.close()"
            quit_command = ["python3", "-c", quit_code, socket_name]
            try:
                if acceleration == "kvm" and os.geteuid() != 0:
                    quit_command = ["sudo", "-n", *quit_command]
                subprocess.run(quit_command, timeout=10, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
                process.wait(timeout=10)
            except (OSError, subprocess.SubprocessError):
                try:
                    os.killpg(process.pid, signal.SIGTERM)
                except PermissionError:
                    pass
                if shutil.which("sudo"):
                    subprocess.run(["sudo", "-n", "kill", "-TERM", "--", f"-{process.pid}"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
            try:
                process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except PermissionError:
                    subprocess.run(["sudo", "-n", "kill", "-KILL", "--", f"-{process.pid}"], check=False)
                process.wait(timeout=10)
        shutil.rmtree(temporary, ignore_errors=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("image", type=Path)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--smoke", action="store_true")
    parser.add_argument("--vm-script", type=Path)
    parser.add_argument("--release-metadata", type=Path)
    parser.add_argument("--boot-log", type=Path)
    parser.add_argument("--timeout", type=int, default=1200)
    arguments = parser.parse_args()
    try:
        result = inspect_image(arguments.image)
        if arguments.smoke:
            require(arguments.vm_script is not None and arguments.boot_log is not None, "Smoke test needs --vm-script and --boot-log")
            result["uefiHttpSmoke"] = smoke_boot(arguments.image, arguments.vm_script, arguments.boot_log, arguments.timeout, arguments.release_metadata)
        arguments.manifest.write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"structuralCheck": result["structuralCheck"], "uefiHttpSmoke": result["uefiHttpSmoke"]}, indent=2))
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        raise SystemExit(f"Image verification failed: {error}") from error


if __name__ == "__main__":
    main()
