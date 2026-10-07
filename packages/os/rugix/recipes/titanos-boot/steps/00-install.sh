#!/bin/bash

set -euo pipefail

BOOT_DIR="${RUGIX_LAYER_DIR}/roots/boot"

mkdir -p "${BOOT_DIR}"

BOOT_TYPE="${RECIPE_PARAM_BOOT_TYPE:-grub}"

case "${BOOT_TYPE}" in
    "grub")
        echo "Copying kernel and initrd..."
        cp -L /vmlinuz "${BOOT_DIR}"
        cp -L /initrd.img "${BOOT_DIR}"
        echo "Installing second stage boot script..."
        cp "${RECIPE_DIR}/files/grub.cfg" "${BOOT_DIR}"
        install -D -m 644 "${RECIPE_DIR}/files/first.grub.cfg" /etc/titan/first.grub.cfg
        install -D -m 755 "${RECIPE_DIR}/files/titan-recovery-menu.py" /usr/libexec/titan-recovery-menu.py
        python3 - "${BOOT_DIR}/titan-version.grubenv" <<'PYTHON'
import json, pathlib, re, sys
release = json.loads(pathlib.Path('/usr/share/titan/release.json').read_text())
version = release['version']
if not re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)', version):
    raise ValueError('Invalid TitanOS boot version')
value = ('# GRUB Environment Block\ntitan_slot_version=' + version + '\n').encode('ascii')
pathlib.Path(sys.argv[1]).write_bytes(value + b'#' * (1024 - len(value)))
PYTHON
        ;;
    "pi")
        echo "Copying firmware files..."
        cp -rp /boot/firmware/* "${BOOT_DIR}"
        ;;
    *)
        echo "Unsupported boot type '${BOOT_TYPE}'."
        exit 1
esac
