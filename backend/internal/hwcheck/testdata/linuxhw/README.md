# Hardware of real machines (linux-hardware.org)

`lspci-sample.json.gz` holds the PCI devices of 799 machines (786 of them
x86-64): Desktops, servers, mini PCs and firewalls with a Linux kernel 6.x,
one report per model, chosen at random (seed 45) from
[linuxhw/LsPCI](https://github.com/linuxhw/LsPCI) at commit `716d5be960ffcac591ebb3309361bdce17cf6079`.

**Source and license:** Hardware probes uploaded by Linux users to
[linux-hardware.org](https://linux-hardware.org), published by linuxhw under
[CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). The file keeps of
each report only machine type, vendor, model, kernel, architecture and per
device class, PCI IDs and the driver/modules of that machine; no probe IDs.
Converted with `scripts/linuxhw-corpus.py`.

`TestLinuxHardwareCorpus` checks how TitanOS's kernel sees these devices and
keeps the storage, network, display, USB and accelerator devices without a
driver in Debian's 6.12 in `missing-6.12.txt` (`-update` after a deliberate
change). To refresh the sample: clone LsPCI without blobs
(`git clone --filter=blob:none --no-checkout --depth 1`), pick paths, check
them out sparsely and run the script.
