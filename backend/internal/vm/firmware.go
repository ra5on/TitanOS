package vm

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func validateFirmware(v VM) error {
	switch v.Firmware {
	case "", "uefi":
		return nil
	case "bios":
		if !v.SecureBoot {
			return nil
		}
	}
	return fmt.Errorf("%w: unsupported firmware or Secure Boot with BIOS", ErrInvalid)
}

// Only UEFI needs writable firmware state. Keep the legacy empty value as UEFI.
func (m *Manager) prepareNVRAM(v VM) error {
	if err := validateFirmware(v); err != nil {
		return err
	}
	if v.Firmware == "bios" {
		return nil
	}
	path := filepath.Join(m.dir(v.Volume, v.folder()), "nvram.fd")
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := copyFile(firmware(v).vars, path); err != nil {
		return err
	}
	return m.share(path, 0o660)
}
