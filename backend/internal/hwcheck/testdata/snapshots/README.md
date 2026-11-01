# Hardware snapshots

Each `*.tar.gz` is a hardware snapshot of a real machine (Diagnose ›
Hardware prüfen › „Schnappschuss herunterladen“, `GET
/api/v1/hardware/snapshot`): the parts of sysfs, procfs and debugfs the
hardware page reads, the kernel log of that start and the PCI/USB module
aliases of its kernel. Serial numbers, MAC and IP addresses are removed.

`TestSnapshots` unpacks every one and checks the hardware page against
`<name>.json` next to it: the hint per device without a driver, the
graphics devices and the disks. A changed result fails the test; after a
deliberate change run

    go test ./internal/hwcheck/ -run TestSnapshots -update

and review the diff of the `.json` files.

| Snapshot | Machine | From |
|---|---|---|
| `qemu-q35.tar.gz` | QEMU q35 VM (virtio, xHCI, ICH9), Debian kernel 6.12.113 | development VM, 01.11.2026 |

Add a snapshot for every reported machine and every fixed hardware bug.
