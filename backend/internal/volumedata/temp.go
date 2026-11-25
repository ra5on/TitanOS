package volumedata

import (
	"crypto/rand"
	"encoding/hex"
	"os"
)

// UnlinkedTemp creates a private scratch file on the opened volume. Its
// descriptor remains usable and the kernel reclaims its space on close or exit.
func UnlinkedTemp(root *os.Root) (*os.File, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	name := ".personal-" + hex.EncodeToString(random[:])
	f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	if err = root.Remove(name); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
