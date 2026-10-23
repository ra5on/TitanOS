// Package volumedata keeps private operational state on an explicitly mounted data volume.
package volumedata

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/ra5on/TitanOS/backend/internal/naming"
)

var ErrUnavailable = errors.New("data volume is unavailable")

// Open pins the mounted volume and refuses symlinked or writable-by-others private directories.
func Open(mountRoot, volume, feature string, mounted []string) (*os.Root, error) {
	if !naming.ValidVolume(volume) || !slices.Contains(mounted, volume) || !naming.ValidVolume(feature) {
		return nil, ErrUnavailable
	}
	point := filepath.Join(mountRoot, volume)
	info, err := os.Lstat(point)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnavailable
	}
	root, err := os.OpenRoot(point)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err := verifyMountedRoot(root, mountRoot, point); err != nil {
		return nil, err
	}
	for _, part := range []string{".titanos", ".titanos/" + feature} {
		if err := root.Mkdir(part, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		st, err := root.Lstat(part)
		if err != nil {
			return nil, err
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok || sys.Uid != uint32(os.Geteuid()) || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0022 != 0 {
			return nil, fmt.Errorf("unsafe private data directory %s", part)
		}
	}
	private, err := root.OpenRoot(".titanos/" + feature)
	if err != nil {
		return nil, err
	}
	if err := SameMount(root, private); err != nil {
		private.Close()
		return nil, err
	}
	return private, nil
}
func ReadJSON(root *os.Root, name string, target any, limit int64) error {
	st, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() > limit {
		return errors.New("invalid state file")
	}
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := io.LimitReader(file, limit+1)
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing state data")
	}
	return nil
}
func WriteJSON(root *os.Root, name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return Write(root, name, append(data, '\n'))
}
func Write(root *os.Root, name string, data []byte) error {
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	tmp := ".pending-" + hex.EncodeToString(id[:])
	file, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = root.Rename(tmp, name); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
