package media

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
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
		// The dictionary size of an .xz stream decides how much memory the
		// decoder allocates. Beyond the limit xz fails instead of allocating.
		cmd := exec.CommandContext(ctx, "xz", "--decompress", "--stdout", "--threads=1", "--memlimit-decompress="+xzMemLimit)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		cmd.Stdin = source
		cmd.Stdout = destination
		var stderr cappedBuffer
		cmd.Stderr = &stderr
		err = cmd.Run()
		if err != nil && strings.Contains(stderr.String(), "Memory usage limit") {
			err = fmt.Errorf("%w: the xz archive needs more than %s of memory to extract", ErrInvalid, xzMemLimit)
		} else if err != nil {
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

// xzMemLimit bounds the memory of the xz decoder (a variable for tests).
var xzMemLimit = "256MiB"

// zipDirectoryBudget bounds what zip.NewReader may read, i.e. the central
// directory it keeps in memory (a variable for tests). Disk image archives
// have a handful of entries; a crafted one could have millions.
var zipDirectoryBudget int64 = 8 << 20

// zipMaxEntries bounds the entry count announced by the end record:
// zip.NewReader preallocates a slice for that many entries.
const zipMaxEntries = 20000

type budgetReaderAt struct {
	r      io.ReaderAt
	budget int64
	armed  bool
}

func (b *budgetReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if b.armed {
		if int64(len(p)) > b.budget {
			return 0, fmt.Errorf("%w: ZIP directory too large", ErrInvalid)
		}
		b.budget -= int64(len(p))
	}
	return b.r.ReadAt(p, off)
}

// zipDirectory reads the entry count and directory size the archive
// announces, the way archive/zip finds them (last valid end record).
func zipDirectory(r io.ReaderAt, size int64) (entries, dirSize uint64, err error) {
	const endLen, locatorLen = 22, 20
	tail := min(size, endLen+0xffff)
	b := make([]byte, tail)
	if _, err := r.ReadAt(b, size-tail); err != nil && !errors.Is(err, io.EOF) {
		return 0, 0, err
	}
	p := -1
	for i := len(b) - endLen; i >= 0; i-- {
		if binary.LittleEndian.Uint32(b[i:]) == 0x06054b50 && i+endLen+int(binary.LittleEndian.Uint16(b[i+20:])) <= len(b) {
			p = i
			break
		}
	}
	if p < 0 {
		return 0, 0, fmt.Errorf("%w: not a ZIP archive", ErrInvalid)
	}
	entries = uint64(binary.LittleEndian.Uint16(b[p+10:]))
	dirSize = uint64(binary.LittleEndian.Uint32(b[p+12:]))
	offset := binary.LittleEndian.Uint32(b[p+16:])
	if entries != 0xffff && dirSize != 0xffffffff && offset != 0xffffffff {
		return entries, dirSize, nil
	}
	end := size - tail + int64(p)
	if end < locatorLen {
		return 0, 0, fmt.Errorf("%w: ZIP64 locator missing", ErrInvalid)
	}
	locator := make([]byte, locatorLen)
	if _, err := r.ReadAt(locator, end-locatorLen); err != nil {
		return 0, 0, err
	}
	if binary.LittleEndian.Uint32(locator) != 0x07064b50 {
		// archive/zip then keeps the 32-bit values.
		return entries, dirSize, nil
	}
	at := int64(binary.LittleEndian.Uint64(locator[8:]))
	record := make([]byte, 56)
	if at < 0 || at+56 > size {
		return 0, 0, fmt.Errorf("%w: bad ZIP64 end record", ErrInvalid)
	}
	if _, err := r.ReadAt(record, at); err != nil {
		return 0, 0, err
	}
	if binary.LittleEndian.Uint32(record) != 0x06064b50 {
		return 0, 0, fmt.Errorf("%w: bad ZIP64 end record", ErrInvalid)
	}
	return binary.LittleEndian.Uint64(record[32:]), binary.LittleEndian.Uint64(record[40:]), nil
}

func extractZIP(file *os.File, size int64, name string, out io.Writer) error {
	entries, dirSize, err := zipDirectory(file, size)
	if err != nil {
		return err
	}
	if entries > zipMaxEntries || dirSize > uint64(zipDirectoryBudget) {
		return fmt.Errorf("%w: ZIP has too many entries for a disk image archive", ErrInvalid)
	}
	limited := &budgetReaderAt{r: file, budget: zipDirectoryBudget + 128<<10, armed: true}
	archive, err := zip.NewReader(limited, size)
	if err != nil {
		return err
	}
	// Entry data is read through the same reader; only the directory is bounded.
	limited.armed = false
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
