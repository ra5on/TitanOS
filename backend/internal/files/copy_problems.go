package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// A copy or move does not stop at the first file it cannot transfer. The
// file is reported with a code, the job goes on, and in a move the source of
// such a file stays where it is. Only problems that concern every following
// file as well (no space, a vanished volume, the job's own records, a cancel)
// end the job. The web interface turns the codes into sentences.

// ConflictMissing copies only files that are missing at the target or differ
// from the file there in size or modification time. Identical files are left
// alone; in a move their source stays and is reported as kept (decision of
// the owner, 4.11.2026: size and time are no proof of equal content).
const ConflictMissing = "missing"

// mtimeTolerance covers FAT, which keeps modification times in two-second steps.
const mtimeTolerance = 2 * time.Second

// maxProblems is how many problem entries a job lists; the counts go on.
const maxProblems = 200

// maxSavedProblems is how many of them a durable job keeps on the volume.
const maxSavedProblems = 50

var (
	// ErrIntoItself: a folder cannot be copied or moved into itself.
	ErrIntoItself = fmt.Errorf("%w: a folder cannot be copied into itself", ErrInvalid)
	// ErrSameFolder: copying into the folder the files are in only works
	// with "keep both", which makes a second copy under a new name.
	ErrSameFolder = errors.New("files: source and destination are the same folder")
	// ErrSourceProtected: this root (the stick) is not a copy source.
	ErrSourceProtected = fmt.Errorf("%w: this location cannot be copied from", ErrProtected)
	// ErrSourceChanged: the source changed while it was being transferred.
	ErrSourceChanged = errors.New("files: the source changed during the transfer")
	// ErrAccess: the operating system refused access.
	ErrAccess = fmt.Errorf("files: access denied: %w", fs.ErrPermission)
	// ErrRebooted: a saved transfer cannot continue after a restart of the NAS.
	ErrRebooted = errors.New("files: the NAS was restarted since the transfer stopped")
)

// CopyProblem is a file or folder a job did not transfer, or left in place.
type CopyProblem struct {
	Path string `json:"path"`
	Code string `json:"code"`
}

// copyFailure carries the problem code of one entry.
type copyFailure struct {
	code string
	err  error
}

func (f *copyFailure) Error() string { return f.err.Error() }
func (f *copyFailure) Unwrap() error { return f.err }
func failure(code string, err error) error {
	return &copyFailure{code: code, err: err}
}

// storeFailure is an error of the job's own records. It ends the job.
type storeFailure struct{ err error }

func (f *storeFailure) Error() string { return "could not save the transfer state: " + f.err.Error() }
func (f *storeFailure) Unwrap() error { return f.err }
func storeErr(err error) error {
	var known *storeFailure
	if err == nil || errors.As(err, &known) || errors.Is(err, context.Canceled) {
		return err
	}
	return &storeFailure{err}
}

// fatalCopy reports errors that end the whole job.
func fatalCopy(err error) bool {
	var store *storeFailure
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrNoSpace) || errors.Is(err, volumedata.ErrUnavailable) ||
		errors.Is(err, ErrNoJob) || errors.As(err, &store)
}

// problemCode names what went wrong with one entry. op is "read" or "write"
// where an input/output error needs telling apart.
func problemCode(err error, op string) string {
	var f *copyFailure
	if errors.As(err, &f) {
		return f.code
	}
	switch {
	case errors.Is(err, syscall.EFBIG):
		return "too_large"
	case errors.Is(err, ErrNameTooLong), errors.Is(err, syscall.ENAMETOOLONG):
		return "name_too_long"
	case errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.EILSEQ):
		return "name_not_allowed"
	case errors.Is(err, syscall.EIO):
		if op != "" {
			return op + "_error"
		}
		return "io_error"
	case errors.Is(err, syscall.EROFS), errors.Is(err, ErrReadOnly):
		return "read_only"
	case errors.Is(err, fs.ErrPermission):
		return "permission"
	case errors.Is(err, ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return "missing"
	case errors.Is(err, ErrSourceChanged), errors.Is(err, ErrTextConflict):
		return "changed"
	case errors.Is(err, ErrExists):
		return "exists"
	case errors.Is(err, ErrIntoItself):
		return "into_itself"
	case errors.Is(err, ErrSameFolder):
		return "same_folder"
	case errors.Is(err, ErrProtected):
		return "protected"
	case errors.Is(err, ErrInvalid):
		return "name_not_allowed"
	}
	return "other"
}

// jobErrorCode names why a whole job stopped.
func jobErrorCode(err error) string {
	var store *storeFailure
	switch {
	case errors.Is(err, ErrNoSpace):
		return "no_space"
	case errors.Is(err, volumedata.ErrUnavailable):
		return "unavailable"
	case errors.As(err, &store):
		return "store"
	case errors.Is(err, ErrIntoItself):
		return "into_itself"
	case errors.Is(err, ErrSameFolder):
		return "same_folder"
	case errors.Is(err, ErrRebooted):
		return "rebooted"
	case errors.Is(err, ErrNotFound):
		return "missing"
	case errors.Is(err, ErrReadOnly), errors.Is(err, fs.ErrPermission):
		return "permission"
	case errors.Is(err, ErrSourceChanged), errors.Is(err, ErrTextConflict):
		return "changed"
	}
	return "other"
}

// noteProblem records an entry the job could not transfer and goes on.
func (m *Manager) noteProblem(job *Job, rel string, err error, op string) error {
	code := problemCode(err, op)
	m.jobs.mu.Lock()
	job.Failed++
	m.addProblemLocked(job, rel, code)
	m.jobs.mu.Unlock()
	return storeErr(m.appendTransfer(job, transferEvent{}))
}

// noteKept records an entry a move left at the source.
func (m *Manager) noteKept(job *Job, rel, code string) error {
	m.jobs.mu.Lock()
	job.Kept++
	m.addProblemLocked(job, rel, code)
	m.jobs.mu.Unlock()
	return storeErr(m.appendTransfer(job, transferEvent{}))
}

func (m *Manager) addProblemLocked(job *Job, rel, code string) {
	if len(job.Problems) < maxProblems {
		job.Problems = append(job.Problems, CopyProblem{Path: rel, Code: code})
	}
	if job.reported == nil {
		job.reported = map[string]bool{}
	}
	if len(job.reported) < 100000 {
		job.reported[rel] = true
	}
}

func (m *Manager) wasReported(job *Job, rel string) bool {
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	return job.reported[rel]
}

// sameContent compares two files the way "missing or changed" does: same
// size and a modification time within two seconds.
func sameContent(a, b fs.FileInfo) bool {
	if !a.Mode().IsRegular() || !b.Mode().IsRegular() || a.Size() != b.Size() {
		return false
	}
	d := a.ModTime().Sub(b.ModTime())
	if d < 0 {
		d = -d
	}
	return d <= mtimeTolerance
}

// copyVersion identifies the state of a source file for a transfer: the
// same file with the same size and modification time. Unlike entryVersion it
// ignores the change time, which moves with every chmod or link count change.
func copyVersion(info fs.FileInfo) string {
	stat, _ := info.Sys().(*syscall.Stat_t)
	if stat == nil {
		return ""
	}
	return fmt.Sprintf("c1:%d:%d:%d:%d", stat.Dev, stat.Ino, info.Size(), info.ModTime().UnixNano())
}
