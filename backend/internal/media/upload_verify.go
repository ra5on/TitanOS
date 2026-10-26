package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
)

type UploadProof struct {
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// VerifyUpload compares every previously uploaded byte in bounded chunks.
func (m *Manager) VerifyUpload(ctx context.Context, id string, proofs []UploadProof) error {
	if len(proofs) == 0 || len(proofs) > 32 {
		return ErrInvalid
	}
	e, err := m.get(id)
	if err != nil {
		return err
	}
	e.op.Lock()
	defer e.op.Unlock()
	j := m.snapshot(e)
	if j.URL != "" || j.SourceReady || !slices.Contains([]string{"uploading", "paused", "failed"}, j.Phase) {
		return ErrInvalid
	}
	m.mu.Lock()
	if e.running {
		m.mu.Unlock()
		return ErrBusy
	}
	e.uploadActive = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); e.uploadActive = false; m.mu.Unlock() }()
	private, err := m.private(j.Volume)
	if err != nil {
		return err
	}
	defer private.Close()
	root, err := secureJob(private, id)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := secureFile(root, "source.part", os.O_RDWR)
	if err != nil {
		return err
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil {
		return err
	}
	if st.Size() < j.Bytes || (j.SourceInode != 0 && st.Sys().(*syscall.Stat_t).Ino != j.SourceInode) {
		return ErrInvalid
	}
	// A chunk may have reached the filesystem before its journal checkpoint.
	// Explicit verification discards only that unpublished, uncommitted tail.
	if st.Size() > j.Bytes {
		if err = file.Truncate(j.Bytes); err != nil {
			return err
		}
		if err = file.Sync(); err != nil {
			return err
		}
	}
	verified := proofs[0].Offset
	if verified != 0 && verified != e.verified {
		return ErrInvalid
	}
	for _, p := range proofs {
		if p.Offset != verified || p.Size <= 0 || p.Size > ChunkBytes || p.Offset+p.Size > j.Bytes || len(p.SHA256) != 64 {
			return ErrInvalid
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		h := sha256.New()
		if _, err = io.CopyBuffer(h, io.NewSectionReader(file, p.Offset, p.Size), make([]byte, 1<<20)); err != nil {
			return err
		}
		if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(p.SHA256) {
			return fmt.Errorf("selected file differs from the already uploaded data")
		}
		verified += p.Size
	}
	e.verified = verified
	return nil
}
