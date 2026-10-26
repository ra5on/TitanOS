package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

func hashFile(ctx context.Context, f *os.File) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := sha256.New()
	buffer := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buffer)
		if n > 0 {
			h.Write(buffer[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type progressWriter struct {
	ctx      context.Context
	file     *os.File
	m        *Manager
	e        *entry
	root     *os.Root
	offset   int64
	written  int64
	last     time.Time
	checked  int64
	download bool
}

func (w *progressWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if w.checked == 0 || w.written-w.checked >= 16<<20 {
		if err := freeSpace(w.file, int64(len(p))); err != nil {
			return 0, err
		}
		w.checked = w.written + 1
	}
	n, err := w.file.Write(p)
	w.written += int64(n)
	w.m.update(w.e, func(j *Job) {
		if w.download {
			j.Bytes = w.offset + w.written
		} else {
			j.Written = w.written
		}
	})
	if time.Since(w.last) > time.Second {
		w.last = time.Now()
		if w.download {
			if err := w.file.Sync(); err != nil {
				return n, err
			}
		}
		if e := w.m.save(w.e, w.root); e != nil {
			return n, e
		}
	}
	return n, err
}

func (m *Manager) download(ctx context.Context, e *entry, root *os.Root) error {
	j := m.snapshot(e)
	file, err := secureFile(root, "source.part", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil {
		return err
	}
	inode := st.Sys().(*syscall.Stat_t).Ino
	if j.SourceInode != 0 && j.SourceInode != inode {
		return fmt.Errorf("source staging file identity changed")
	}
	// Only checkpointed bytes are known durable across power loss. Ignore an
	// unjournaled tail even if its inode size was already committed by the FS.
	offset := min(st.Size(), j.Bytes)
	validator := j.ETag
	if strings.HasPrefix(validator, "W/") {
		validator = ""
	}
	if validator == "" {
		validator = j.LastModified
	}
	if offset > 0 && (validator == "" || j.Bytes == 0) {
		offset = 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.URL, nil)
	if err != nil {
		return ErrInvalid
	}
	if j.URL == VirtIOURL {
		req.Header.Set("User-Agent", "TitanOS VM media importer")
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		req.Header.Set("If-Range", validator)
	}
	client := *m.Client
	if j.URL == VirtIOURL {
		original := client.CheckRedirect
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if req.URL.User != nil || req.URL.Host != "fedorapeople.org" || !strings.HasPrefix(req.URL.Path, "/groups/virt/virtio-win/direct-downloads/") || (req.URL.Scheme != "https" && req.URL.Scheme != "http") {
				return ErrInvalid
			}
			// Fedora's official alias currently emits http Location headers even
			// on HTTPS. Upgrade that same-origin path before any request leaves
			// the client; never send the ISO download over plaintext HTTP.
			if req.URL.Scheme == "http" {
				u := *req.URL
				u.Scheme = "https"
				req.URL = &u
			}
			if original != nil {
				return original(req, via)
			}
			return nil
		}
	}
	res, err := client.Do(req)
	if err != nil {
		if u, ok := err.(*url.Error); ok {
			return fmt.Errorf("download failed: %w", u.Err)
		}
		return fmt.Errorf("download failed")
	}
	defer res.Body.Close()
	total := res.ContentLength
	switch res.StatusCode {
	case http.StatusOK:
		offset = 0
	case http.StatusPartialContent:
		var start, end int64
		var count int
		count, err = fmt.Sscanf(res.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total)
		if err != nil || count != 3 || start != offset || end < start || total <= end || (res.ContentLength >= 0 && res.ContentLength != end-start+1) {
			return fmt.Errorf("invalid HTTP resume range")
		}
		if offset > 0 && ((j.ETag != "" && res.Header.Get("ETag") != j.ETag) || (j.ETag == "" && j.LastModified != "" && res.Header.Get("Last-Modified") != j.LastModified)) {
			return fmt.Errorf("source changed during resume; retry the import")
		}
	default:
		return fmt.Errorf("download returned HTTP %d", res.StatusCode)
	}
	if err = file.Truncate(offset); err != nil {
		return err
	}
	if _, err = file.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	if total > offset {
		if err = freeSpace(file, total-offset); err != nil {
			return err
		}
	}
	m.update(e, func(j *Job) {
		j.SourceInode = inode
		j.Bytes = offset
		j.Total = max(0, total)
		j.ETag = res.Header.Get("ETag")
		j.LastModified = res.Header.Get("Last-Modified")
	})
	if err = m.save(e, root); err != nil {
		return err
	}
	writer := &progressWriter{ctx: ctx, file: file, m: m, e: e, root: root, offset: offset, download: true}
	if _, err = io.CopyBuffer(writer, res.Body, make([]byte, 1<<20)); err != nil {
		return errors.Join(err, file.Sync())
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if total >= 0 && offset+writer.written != total {
		return fmt.Errorf("incomplete HTTP response")
	}
	m.update(e, func(j *Job) {
		j.Bytes = offset + writer.written
		j.Total = offset + writer.written
		j.SourceReady = true
	})
	return m.save(e, root)
}
