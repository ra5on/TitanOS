<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/titanos-wordmark-dark-bg.svg">
    <img src="docs/brand/titanos-wordmark-light-bg.svg" alt="TitanOS" width="420">
  </picture>
</p>

<p align="center"><strong>A lean NAS operating system that boots from a USB stick and runs entirely in RAM.</strong></p>

---

> **Status:** early development – nothing to install yet. Follow the repository for the first release.

## What TitanOS is

- **Boots from USB, runs in RAM.** The system image is a compressed squashfs that is copied into RAM at boot. The stick is only touched for configuration changes and updates.
- **Classic RAID.** RAID 0, 1, 5, 6 and 10 via Linux software RAID (mdadm), with ext4 or XFS; optional Btrfs mirrors with snapshots.
- **Docker with an app store.** Add your own app sources; templates fill in variables, paths and ports automatically. Edit every container as a form or as Compose YAML.
- **Virtual machines.** KVM/QEMU with UEFI, qcow2 or raw disks, GPU/iGPU passthrough, NAS folders shared into VMs via virtiofs, browser console (VNC) with fullscreen.
- **Hardware aware.** Detects Intel iGPU and NPU and NVIDIA GPUs and lets you decide whether containers or a VM may use them. NVIDIA driver as a one-click plugin.
- **Safe updates.** Signed updates from GitHub Releases into an A/B slot on the stick, with automatic rollback if the new version does not boot.
- **Your look.** Themes, accent colors, glass effect, corner radius and a freely arrangeable dashboard.

Based on Debian. Web UI in German and English. Inspired by Unraid – an independent project, not affiliated with Lime Technology.

## Installation (planned)

1. Format a USB stick (8–32 GB) as FAT32 with the label `TITANOS`.
2. Download the latest `titanos-x.y.z.zip` from [Releases](https://github.com/ra5on/TitanOS/releases), verify the checksum and extract it onto the stick.
3. Boot your NAS from the stick (UEFI, Secure Boot off) and open `https://<nas-ip>` in a browser. The certificate is self-signed, so the browser asks you to confirm it once.

## Development

Requirements: Go 1.27+, Node.js 24+ and `make`.
For the system image additionally (Debian 13): `mkosi squashfs-tools mtools zip systemd-boot-efi systemd-container`, and `qemu-system-x86 ovmf` for testing. No root needed.

| Command | What it does |
|---|---|
| `make build` | Builds the web UI and `backend/bin/titand` with the UI embedded |
| `make test` | Runs the Go tests and the type check of the web UI |
| `make dev` | Starts `titand` in development mode on `127.0.0.1:8080` (plain HTTP, configuration in `test/config/`) and the Vite dev server with hot reload |
| `make zip` | Builds the system image and packs the stick contents: `out/stick/` and `out/titanos-<version>.zip` |
| `make zip-dev` | Same, as a development build with root logged in automatically on the console |
| `make qemu` | Boots `out/stick/` in QEMU (UEFI) from an emulated USB stick; web UI on `https://127.0.0.1:8443` (self-signed certificate) |

The backend (`backend/`, Go) and the web UI (`web/`, Svelte + TypeScript) live in this repository. A plain `go build` or `go test` works without Node.js and contains a placeholder page instead of the web UI.

## License

TitanOS is **Fair Source** under the [Functional Source License 1.1 (FSL-1.1-ALv2)](LICENSE.md):

- Free to use, modify and share – privately, in a company, for education and research.
- Not allowed: offering TitanOS or a product substantially based on it as a competing commercial product or service.
- Each version becomes available under the **Apache License 2.0** two years after its release.

Third-party components (Linux kernel, Debian packages and others) keep their own licenses, see [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES).

## Contributing

Contributions are welcome – please read [CONTRIBUTING.md](CONTRIBUTING.md) first. Security issues: see [SECURITY.md](SECURITY.md).
