package files

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type moveFixture struct {
	base, user string
	src, dst   Root
}

func (f moveFixture) manager() *Manager {
	m := NewManager(os.Getgid())
	m.MountRoot = f.base
	m.Mounted = func() []string { return []string{"source", "internal"} }
	return m
}

func newMoveFixture(t *testing.T, kind string) moveFixture {
	t.Helper()
	base := t.TempDir()
	if cmdline, _ := os.ReadFile("/proc/cmdline"); strings.Contains(" "+strings.TrimSpace(string(cmdline))+" ", " titanos.move-test=1 ") {
		base = "/move-qa" // three disposable guest disks, mounted by the test image
	}
	place := func(volume string) string {
		root := filepath.Join(base, volume)
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		dir, err := os.MkdirTemp(root, "move-")
		if err != nil {
			t.Fatal(err)
		}
		return dir
	}
	src, dst := place("source"), place(kind)
	return moveRoots(base, kind, filepath.Base(src), src, dst)
}

func moveRoots(base, kind, user, sourceDir, targetDir string) moveFixture {
	src := Root{ID: "share/source", Kind: "share", Volume: "source", Dir: sourceDir, Writable: true}
	dst := Root{ID: "share/internal", Kind: "share", Volume: "internal", Dir: targetDir, Writable: true}
	if kind == "usb" {
		dst.ID, dst.Kind, dst.Volume = "usb/test-stick", "usb", ""
	}
	return moveFixture{base, user, src, dst}
}

func (f moveFixture) start(t *testing.T, m *Manager, name string) Job {
	t.Helper()
	j, err := m.StartCopy(f.src, []string{name}, f.dst, ".", true, ConflictRename, f.user, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	return finishJob(t, m, f.user, j.ID)
}

func TestMovePublicationBarriers(t *testing.T) {
	for _, kind := range []string{"internal", "usb"} {
		t.Run(kind, func(t *testing.T) {
			for _, failAt := range []string{"tree", "tree/sub", "tree/sub/note (2).txt", ""} {
				name := failAt
				if name == "" {
					name = "success"
				}
				t.Run(name, func(t *testing.T) {
					f := newMoveFixture(t, kind)
					m := f.manager()
					const rel = "tree/sub/note.txt"
					const dest = "tree/sub/note (2).txt"
					put(t, filepath.Join(f.src.Dir, rel), "original")
					put(t, filepath.Join(f.dst.Dir, rel), "existing")
					m.syncCopyParent = func(r *os.Root, p string) error {
						if r.Name() == f.dst.Dir && p == failAt {
							return syscall.EIO
						}
						return syncTransferParent(r, p)
					}
					job := f.start(t, m, "tree")
					if !job.Durable || read(t, filepath.Join(f.dst.Dir, rel)) != "existing" {
						t.Fatalf("lost plan or overwrote existing target: %+v", job)
					}
					if failAt != "" {
						if job.State != JobFailed || read(t, filepath.Join(f.src.Dir, rel)) != "original" {
							t.Fatalf("move acknowledged before directory sync: %+v", job)
						}
					} else if job.State != JobDone || job.Failed != 0 || job.Kept != 0 || exists(filepath.Join(f.src.Dir, "tree")) || read(t, filepath.Join(f.dst.Dir, dest)) != "original" {
						t.Fatalf("incomplete move: %+v", job)
					}
					jobs := f.manager().Jobs(f.user)
					if len(jobs) != 1 || jobs[0].State != job.State {
						t.Fatalf("reopened plan: %+v", jobs)
					}
				})
			}
		})
	}
}

func TestMoveKeepsSourceIfPublishedTargetIsReplaced(t *testing.T) {
	for _, kind := range []string{"usb", "internal"} {
		t.Run(kind, func(t *testing.T) {
			f := newMoveFixture(t, kind)
			m := f.manager()
			put(t, filepath.Join(f.src.Dir, "note"), "original")
			put(t, filepath.Join(f.dst.Dir, "another"), "other client")
			m.syncCopyParent = func(r *os.Root, p string) error {
				if r.Name() == f.dst.Dir && p == "note" {
					if err := r.Rename("another", p); err != nil {
						return err
					}
				}
				return syncTransferParent(r, p)
			}
			job := f.start(t, m, "note")
			if job.State != JobFailed || read(t, filepath.Join(f.src.Dir, "note")) != "original" || read(t, filepath.Join(f.dst.Dir, "note")) != "other client" {
				t.Fatal(job)
			}
		})
	}
}

// The worker exits without running defers after rename, before its directory
// sync or completion transaction. The parent reopens the actual bbolt file.
func TestMoveCrashAtPublication(t *testing.T) {
	if base := os.Getenv("TITANOS_MOVE_CHILD_BASE"); base != "" {
		kind := os.Getenv("TITANOS_MOVE_CHILD_KIND")
		user := os.Getenv("TITANOS_MOVE_CHILD_USER")
		f := moveRoots(base, kind, user, filepath.Join(base, "source", user), os.Getenv("TITANOS_MOVE_CHILD_TARGET"))
		m := f.manager()
		m.syncCopyParent = func(r *os.Root, p string) error {
			if r.Name() == f.dst.Dir && p == "note" {
				os.Exit(76)
			}
			return syncTransferParent(r, p)
		}
		t.Fatal("missing crash point", f.start(t, m, "note"))
	}
	for _, kind := range []string{"internal", "usb"} {
		t.Run(kind, func(t *testing.T) {
			f := newMoveFixture(t, kind)
			put(t, filepath.Join(f.src.Dir, "note"), "safe original")
			cmd := exec.Command(os.Args[0], "-test.run=^TestMoveCrashAtPublication$")
			cmd.Env = append(os.Environ(), "TITANOS_MOVE_CHILD_BASE="+f.base, "TITANOS_MOVE_CHILD_USER="+f.user, "TITANOS_MOVE_CHILD_KIND="+kind, "TITANOS_MOVE_CHILD_TARGET="+f.dst.Dir)
			out, err := cmd.CombinedOutput()
			var status *exec.ExitError
			if !errors.As(err, &status) || status.ExitCode() != 76 {
				t.Fatalf("worker %v: %s", err, out)
			}
			m := f.manager()
			jobs := m.Jobs(f.user)
			if len(jobs) != 1 || !jobs[0].Blocked || jobs[0].Retryable || jobs[0].ErrorCode != "unknown_outcome" || jobs[0].State != JobFailed {
				t.Fatal(jobs)
			}
			if _, err := m.RetryCopy(f.user, jobs[0].ID, f.src, f.dst, os.Getuid()); err == nil {
				t.Fatal("replayed unknown publication")
			}
			if read(t, filepath.Join(f.src.Dir, "note")) != "safe original" || read(t, filepath.Join(f.dst.Dir, "note")) != "safe original" {
				t.Fatal("incomplete copies")
			}
		})
	}
}

// The VM harness restarts the guest without flushing its filesystems at the
// marker, then reruns this test on the same three emulated drives.
func TestGuestMoveAcrossPowerCut(t *testing.T) {
	cmdline, _ := os.ReadFile("/proc/cmdline")
	if !strings.Contains(" "+strings.TrimSpace(string(cmdline))+" ", " titanos.move-test=1 ") || os.Getenv("MOVE_CUT_PHASE") == "" {
		t.Skip("requires the isolated disk-test VM")
	}
	kind, cut := os.Getenv("MOVE_CUT_KIND"), os.Getenv("MOVE_CUT_POINT")
	if kind != "internal" && kind != "usb" || cut != "before-target-sync" && cut != "after-source-unlink" {
		t.Fatal("invalid fixture")
	}
	const base = "/move-qa"
	user := kind + "-" + cut
	f := moveRoots(base, kind, user, filepath.Join(base, "source", user), filepath.Join(base, kind, user))
	m := f.manager()
	const original = "acknowledged before the move"
	src, dst := filepath.Join(f.src.Dir, "letter"), filepath.Join(f.dst.Dir, "letter (2)")
	existing := filepath.Join(f.dst.Dir, "letter")
	switch os.Getenv("MOVE_CUT_PHASE") {
	case "arm":
		for _, dir := range []string{f.src.Dir, f.dst.Dir} {
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
		}
		put(t, src, original)
		put(t, existing, "untouched")
		for _, dir := range []string{f.src.Dir, f.dst.Dir} {
			fd, err := os.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			err = errors.Join(unix.Syncfs(int(fd.Fd())), fd.Close())
			if err != nil {
				t.Fatal(err)
			}
		}
		m.syncCopyParent = func(r *os.Root, p string) error {
			if cut == "before-target-sync" && r.Name() == f.dst.Dir && p == "letter (2)" || cut == "after-source-unlink" && r.Name() == f.src.Dir && p == "letter" {
				if err := os.WriteFile("/tmp/reset-guest", nil, 0600); err != nil {
					return err
				}
				// init now resets this guest. Prevent worker progress while it does so.
				time.Sleep(90 * time.Second)
				return errors.New("guest reset did not happen")
			}
			return syncTransferParent(r, p)
		}
		if _, err := m.StartCopy(f.src, []string{"letter"}, f.dst, ".", true, ConflictRename, f.user, os.Getuid()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(90 * time.Second)
		t.Fatal("guest reset did not happen")
	case "check":
		if read(t, existing) != "untouched" {
			t.Fatal("overwritten")
		}
		source, sourceErr := os.ReadFile(src)
		target, targetErr := os.ReadFile(dst)
		if cut == "before-target-sync" && (sourceErr != nil || string(source) != original) {
			t.Fatalf("lost original: %q, %v", source, sourceErr)
		}
		if cut == "after-source-unlink" && (targetErr != nil || string(target) != original) {
			t.Fatalf("lost published copy: %q, %v", target, targetErr)
		}
		for _, item := range []struct {
			data []byte
			err  error
		}{{source, sourceErr}, {target, targetErr}} {
			if !errors.Is(item.err, os.ErrNotExist) && (item.err != nil || string(item.data) != original) {
				t.Fatalf("partial file: %q, %v", item.data, item.err)
			}
		}
		jobs := m.Jobs(user)
		if len(jobs) != 1 || jobs[0].State != JobFailed || !jobs[0].Rebooted || jobs[0].Retryable || jobs[0].Blocked != (cut == "before-target-sync") {
			t.Fatalf("incorrect recovered checkpoint: %+v", jobs)
		}
	default:
		t.Fatal("invalid phase")
	}
}
