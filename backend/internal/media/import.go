package media

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"golang.org/x/sys/unix"
)

func (m *Manager) work(ctx context.Context, e *entry) {
	var workErr error
	var root *os.Root
	defer func() {
		m.mu.Lock()
		if workErr != nil {
			if e.job.Phase == "publishing" || e.job.Phase == "done" {
				e.job.Phase = "review"
				e.job.Error = "Publication interrupted; inspect output before retrying"
			} else if e.desired != "" {
				e.job.Phase = e.desired
				e.job.Error = ""
			} else {
				e.job.Phase = "failed"
				e.job.Error = workErr.Error()
			}
		}
		m.mu.Unlock()
		if root != nil {
			if m.snapshot(e).Phase == "cancelled" {
				root.Remove("source.part")
				root.Remove("image.part")
			}
			if err := m.save(e, root); err != nil {
				m.update(e, func(j *Job) { j.Error = "Could not save import checkpoint: " + err.Error() })
			}
			root.Close()
		}
		m.mu.Lock()
		e.running = false
		e.cancel = nil
		close(e.done)
		m.mu.Unlock()
		m.wg.Done()
	}()
	private, err := m.private(m.snapshot(e).Volume)
	if err != nil {
		workErr = err
		return
	}
	defer private.Close()
	root, err = secureJob(private, m.snapshot(e).ID)
	if err != nil {
		workErr = err
		return
	}
	j := m.snapshot(e)
	if !j.SourceReady {
		if j.URL != "" {
			if err = m.download(ctx, e, root); err != nil {
				workErr = err
				return
			}
		} else if j.Bytes != j.UploadSize {
			workErr = ErrInvalid
			return
		} else {
			m.update(e, func(j *Job) { j.SourceReady = true })
		}
	}
	source, err := secureFile(root, "source.part", os.O_RDONLY)
	if err != nil {
		workErr = err
		return
	}
	defer source.Close()
	st, err := source.Stat()
	if err != nil || st.Size() == 0 {
		workErr = ErrInvalid
		return
	}
	if j.SourceInode != 0 && st.Sys().(*syscall.Stat_t).Ino != j.SourceInode {
		workErr = fmt.Errorf("source staging file identity changed")
		return
	}
	m.update(e, func(j *Job) { j.Phase = "verifying" })
	hash, err := hashFile(ctx, source)
	if err != nil {
		workErr = err
		return
	}
	j = m.snapshot(e)
	if j.ExpectedSHA256 != "" && hash != j.ExpectedSHA256 {
		workErr = fmt.Errorf("source SHA-256 mismatch")
		return
	}
	m.update(e, func(j *Job) { j.SourceSHA256 = hash; j.Phase = "extracting"; j.Written = 0 })
	if err = m.save(e, root); err != nil {
		workErr = err
		return
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		workErr = err
		return
	}
	output, err := secureFile(root, "image.part", os.O_CREATE|os.O_TRUNC|os.O_RDWR)
	if err != nil {
		workErr = err
		return
	}
	defer output.Close()
	writer := &progressWriter{ctx: ctx, file: output, m: m, e: e, root: root}
	digest := sha256.New()
	destination := io.MultiWriter(writer, digest)
	switch j.Compression {
	case "none":
		_, err = io.CopyBuffer(destination, source, make([]byte, 1<<20))
	case "gz":
		var reader *gzip.Reader
		reader, err = gzip.NewReader(source)
		if err == nil {
			_, err = io.CopyBuffer(destination, reader, make([]byte, 1<<20))
			err = errors.Join(err, reader.Close())
		}
	case "xz":
		cmd := exec.CommandContext(ctx, "xz", "-dc")
		cmd.Stdin = source
		cmd.Stdout = destination
		var stderr cappedBuffer
		cmd.Stderr = &stderr
		err = cmd.Run()
		if err != nil {
			err = fmt.Errorf("xz extraction failed: %w", err)
		}
	case "zip":
		err = extractZIP(source, st.Size(), j.Name, destination)
	default:
		err = ErrInvalid
	}
	if err != nil {
		workErr = err
		return
	}
	if err = output.Sync(); err != nil {
		workErr = err
		return
	}
	outStat, err := output.Stat()
	if err != nil || outStat.Size() == 0 {
		workErr = ErrInvalid
		return
	}
	m.update(e, func(j *Job) {
		j.Phase = "verifying"
		j.OutputBytes = outStat.Size()
		j.SHA256 = hex.EncodeToString(digest.Sum(nil))
	})
	if err = validateImage(ctx, output, j.Name); err != nil {
		workErr = err
		return
	}
	if err = ctx.Err(); err != nil {
		workErr = err
		return
	}
	m.mu.Lock()
	if e.desired != "" || ctx.Err() != nil {
		m.mu.Unlock()
		workErr = context.Canceled
		return
	}
	e.job.Phase = "publishing"
	m.mu.Unlock()
	if err = m.save(e, root); err != nil {
		workErr = err
		return
	}
	if err = m.publish(e, private, root, output); err != nil {
		workErr = err
		return
	}
	m.update(e, func(j *Job) {
		j.Phase = "done"
		j.Error = ""
		j.Output = filepath.Join(m.MountRoot, j.Volume, "iso", j.Name)
	})
	if err = m.save(e, root); err != nil {
		workErr = err
		return
	}
	source.Close()
	root.Remove("source.part")
}

func extractZIP(file *os.File, size int64, name string, out io.Writer) error {
	archive, err := zip.NewReader(file, size)
	if err != nil {
		return err
	}
	var image *zip.File
	for _, entry := range archive.File {
		path := strings.TrimSuffix(entry.Name, "/")
		if !fs.ValidPath(path) || strings.Contains(path, "\\") || entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe ZIP entry")
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if !entry.Mode().IsRegular() {
			return ErrInvalid
		}
		if supported(entry.Name) {
			if image != nil {
				return fmt.Errorf("ZIP must contain exactly one supported image")
			}
			image = entry
		}
	}
	if image == nil || !strings.EqualFold(filepath.Ext(image.Name), filepath.Ext(name)) {
		return fmt.Errorf("ZIP needs exactly one image matching the output filename type")
	}
	reader, err := image.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	_, err = io.CopyBuffer(out, reader, make([]byte, 1<<20))
	return err
}

type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		return 0, fmt.Errorf("command output limit exceeded")
	}
	return b.Buffer.Write(p)
}
func validateImage(ctx context.Context, file *os.File, name string) error {
	if strings.EqualFold(filepath.Ext(name), ".iso") {
		var signature [5]byte
		for sector := int64(16); sector < 32; sector++ {
			n, _ := file.ReadAt(signature[:], sector*2048+1)
			if n == 5 && (string(signature[:]) == "CD001" || string(signature[:]) == "NSR02" || string(signature[:]) == "NSR03") {
				return nil
			}
		}
		return fmt.Errorf("file has no ISO9660/UDF volume descriptor")
	}
	formats := map[string]string{".qcow2": "qcow2", ".raw": "raw", ".img": "raw", ".vmdk": "vmdk", ".vhdx": "vhdx", ".vdi": "vdi"}
	format := formats[strings.ToLower(filepath.Ext(name))]
	if format == "" {
		return ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(bounded, "qemu-img", "info", "--output=json", "-f", format, "/proc/self/fd/3")
	cmd.ExtraFiles = []*os.File{file}
	var stdout, stderr cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("image format check failed: %w", err)
	}
	var info map[string]any
	if json.Unmarshal(stdout.Bytes(), &info) != nil {
		return ErrInvalid
	}
	if size, ok := info["virtual-size"].(float64); !ok || size <= 0 {
		return ErrInvalid
	}
	if info["backing-filename"] != nil && info["backing-filename"] != "" {
		return fmt.Errorf("images with a backing file are not supported")
	}
	if hasExternalData(info) {
		return fmt.Errorf("images with external data files are not supported")
	}
	return nil
}
func hasExternalData(v any) bool {
	switch value := v.(type) {
	case map[string]any:
		for k, v := range value {
			if k == "data-file" {
				if s, ok := v.(string); ok && s != "" {
					return true
				}
			}
			if k == "filename" {
				if s, ok := v.(string); ok && s != "/proc/self/fd/3" && s != "" {
					return true
				}
			}
			if hasExternalData(v) {
				return true
			}
		}
	case []any:
		for _, v := range value {
			if hasExternalData(v) {
				return true
			}
		}
	}
	return false
}

func (m *Manager) publish(e *entry, private, jobRoot *os.Root, file *os.File) error {
	j := m.snapshot(e)
	if !slices.Contains(m.Mounted(), j.Volume) {
		return ErrUnavailable
	}
	volume, err := os.OpenRoot(filepath.Join(m.MountRoot, j.Volume))
	if err != nil {
		return err
	}
	defer volume.Close()
	if err = volumedata.SameMount(private, volume); err != nil {
		return err
	}
	if err = volume.Mkdir("iso", 0755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	st, err := volume.Lstat("iso")
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return ErrInvalid
	}
	target, err := volume.OpenRoot("iso")
	if err != nil {
		return err
	}
	defer target.Close()
	if err = volumedata.SameMount(private, target); err != nil {
		return err
	}
	if err = file.Chown(-1, m.GID); err != nil {
		return err
	}
	if err = file.Chmod(0640); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	src, err := jobRoot.Open(".")
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := target.Open(".")
	if err != nil {
		return err
	}
	defer dst.Close()
	if err = unix.Renameat2(int(src.Fd()), "image.part", int(dst.Fd()), j.Name, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("output was not published (existing files are never overwritten): %w", err)
	}
	if err = dst.Sync(); err != nil {
		return err
	}
	return src.Sync()
}
