package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// A fault after the first published name makes a genuinely mixed visible tree.
// The recovery tree must still contain the previous pair, even after reopening
// the target and trying (and interrupting) the next run.
type afterPublication struct {
	context.Context
	trigger func()
}

func (c *afterPublication) Err() error {
	c.trigger()
	return c.Context.Err()
}

func TestUndatedRecoveryKeepsMatchingPair(t *testing.T) {
	for _, mode := range []string{"copy", "mirror"} {
		for _, fault := range []string{"cancel", "full-ext4", "cancel-exfat"} {
			t.Run(mode+"/"+fault, func(t *testing.T) {
				parent := t.TempDir()
				if fault != "cancel" {
					cmdline, _ := os.ReadFile("/proc/cmdline")
					if !strings.Contains(" "+strings.TrimSpace(string(cmdline))+" ", " titanos.move-test=1 ") {
						t.Skip("disposable QEMU ext4/USB-exFAT image required")
					}
					mount := "/move-qa/backup"
					if fault == "cancel-exfat" {
						mount = "/move-qa/usb"
					}
					var err error
					parent, err = os.MkdirTemp(mount, "undated-test-")
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := os.RemoveAll(parent); err != nil {
							t.Error(err)
						}
					})
				}
				source := t.TempDir()
				job := Job{ID: "pair", Sources: []string{"docs"}, Copy: &CopyOptions{Mirror: mode == "mirror"}, Destination: Destination{Type: "usb-copy", Folder: "backup"}}
				stateDir := t.TempDir()
				fresh := func() *Manager {
					m := NewManager(config.Open(stateDir))
					m.mountRoot = t.TempDir()
					m.Sources = func() []Source { return []Source{{Name: "docs", Volume: "tank", Path: source}} }
					return m
				}
				run := func(m *Manager, ctx context.Context) error {
					root, err := os.OpenRoot(parent)
					if err != nil {
						t.Fatal(err)
					}
					defer root.Close()
					return m.copyTo(ctx, job, root)
				}
				write := func(path string, b []byte) {
					t.Helper()
					if err := os.WriteFile(path, b, 0600); err != nil {
						t.Fatal(err)
					}
				}
				expect := func(path string, b []byte) {
					t.Helper()
					raw, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(raw, b) {
						t.Fatalf("%s: %v; read %d bytes, wanted matching %d bytes", path, err, len(raw), len(b))
					}
				}
				a, b := "catalog", "records"
				oneA, oneB := []byte("generation-1:records"), []byte("generation-1:rows")
				twoA, twoB := []byte("generation-2:records"), bytes.Repeat([]byte("generation-2:rows\n"), 200000)
				write(filepath.Join(source, a), oneA)
				write(filepath.Join(source, b), oneB)
				if err := run(fresh(), context.Background()); err != nil {
					t.Fatal(err)
				}
				live := filepath.Join(parent, "backup", "docs")
				foreign := filepath.Join(parent, "backup", "external.txt")
				write(foreign, []byte("belongs to someone else"))
				write(filepath.Join(source, a), twoA)
				write(filepath.Join(source, b), twoB)
				var triggered bool
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				ballast := filepath.Join(parent, "ballast")
				faultCtx := &afterPublication{Context: ctx, trigger: func() {
					if triggered {
						return
					}
					got, _ := os.ReadFile(filepath.Join(live, a))
					if !bytes.Equal(got, twoA) {
						return
					}
					triggered = true
					if fault != "full-ext4" {
						cancel()
						return
					}
					// Only this fresh 128 MiB VM image is filled. Exhaust the
					// remaining space for the second file, after the preflight.
					f, err := os.Create(ballast)
					if err != nil {
						t.Fatal(err)
					}
					defer f.Close()
					for step := int64(512 << 10); step >= 4096; step /= 2 {
						for {
							offset, err := f.Seek(0, io.SeekEnd)
							if err != nil {
								t.Fatal(err)
							}
							if offset >= 128<<20 {
								t.Fatal("unexpected disk capacity")
							}
							err = syscall.Fallocate(int(f.Fd()), 0, offset, step)
							if errors.Is(err, syscall.ENOSPC) {
								break
							}
							if err != nil {
								t.Fatal(err)
							}
						}
					}
				}}
				err := run(fresh(), faultCtx)
				want := error(context.Canceled)
				if fault == "full-ext4" {
					want = syscall.ENOSPC
				}
				if !triggered || !errors.Is(err, want) {
					t.Fatalf("triggered=%v, err=%v, wanted %v", triggered, err, want)
				}
				expect(filepath.Join(live, a), twoA)
				expect(filepath.Join(live, b), oneB)
				root, err := os.OpenRoot(parent)
				if err != nil {
					t.Fatal(err)
				}
				recovery := recoveryFor(job, root)
				previous := filepath.Join(parent, recovery.base, "previous", "docs")
				root.Close()
				expect(filepath.Join(previous, a), oneA)
				expect(filepath.Join(previous, b), oneB)
				expect(foreign, []byte("belongs to someone else"))
				if fault == "full-ext4" {
					if err := os.Remove(ballast); err != nil {
						t.Fatal(err)
					}
				}
				// A second interruption after another visible replacement may
				// not replace the durable checkpoint with a mixed tree.
				threeA := []byte("generation-3:records")
				write(filepath.Join(source, a), threeA)
				retry, interrupt := context.WithCancel(context.Background())
				defer interrupt()
				if err := run(fresh(), &afterPublication{Context: retry, trigger: func() {
					got, _ := os.ReadFile(filepath.Join(live, a))
					if bytes.Equal(got, threeA) {
						interrupt()
					}
				}}); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				expect(filepath.Join(previous, a), oneA)
				expect(filepath.Join(previous, b), oneB)
				// Restore the complete previous pair into an empty directory before retry.
				dest := t.TempDir()
				for _, name := range []string{a, b} {
					body, err := os.ReadFile(filepath.Join(previous, name))
					if err != nil {
						t.Fatal(err)
					}
					write(filepath.Join(dest, name), body)
				}
				expect(filepath.Join(dest, a), oneA)
				expect(filepath.Join(dest, b), oneB)
				if err := run(fresh(), context.Background()); err != nil {
					t.Fatal(err)
				}
				expect(filepath.Join(live, a), threeA)
				expect(filepath.Join(live, b), twoB)
				if _, err := os.Stat(previous); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("retired checkpoint: %v", err)
				}
				expect(foreign, []byte("belongs to someone else"))
			})
		}
	}
}

func TestUndatedRecoveryRejectsForeignFolder(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	job := Job{ID: "plain", Sources: []string{"docs"}, Destination: Destination{Folder: "backup"}}
	r := recoveryFor(job, root)
	if err := root.MkdirAll(filepath.Join(r.base, "building"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(filepath.Join(r.base, "building", "mine"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.prepare(context.Background(), job); err == nil {
		t.Fatal("took over a foreign folder")
	}
	if err := r.discardStaging(job); err == nil {
		t.Fatal("cleaned a foreign folder")
	}
	if got, err := root.ReadFile(filepath.Join(r.base, "building", "mine")); err != nil || string(got) != "private" {
		t.Fatalf("foreign file: %q %v", got, err)
	}
}

// The VM resets without sync after the first file of the new pair is visible.
// A second boot reads the previous pair from the same disposable filesystem.
func TestGuestUndatedRecoveryAcrossPowerCut(t *testing.T) {
	cmdline, _ := os.ReadFile("/proc/cmdline")
	if !strings.Contains(" "+strings.TrimSpace(string(cmdline))+" ", " titanos.move-test=1 ") || os.Getenv("MOVE_CUT_POINT") != "readable-copy" {
		t.Skip("requires the isolated disk-test VM")
	}
	kind := os.Getenv("MOVE_CUT_KIND")
	if kind != "internal" && kind != "usb" {
		t.Fatal("unknown target")
	}
	const base = "/move-qa"
	source := filepath.Join(base, "source", "recovery-"+kind)
	root, err := os.OpenRoot(filepath.Join(base, kind))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	job := Job{ID: "pair", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "powercut-pair"}, Copy: &CopyOptions{Mirror: kind == "usb"}}
	m := NewManager(config.Open(t.TempDir()))
	m.mountRoot = t.TempDir()
	m.Sources = func() []Source { return []Source{{Name: "docs", Volume: "tank", Path: source}} }
	live := filepath.Join(job.Destination.Folder, "docs")
	check := func(folder, prefix string) {
		t.Helper()
		for _, name := range []string{"catalog", "records"} {
			got, err := root.ReadFile(filepath.Join(folder, name))
			if err != nil || string(got) != prefix+name {
				t.Fatalf("%s/%s: %q %v", folder, name, got, err)
			}
		}
	}
	writePair := func(prefix string) {
		t.Helper()
		for _, name := range []string{"catalog", "records"} {
			if err := os.WriteFile(filepath.Join(source, name), []byte(prefix+name), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	switch os.Getenv("MOVE_CUT_PHASE") {
	case "arm":
		if err := os.Mkdir(source, 0700); err != nil {
			t.Fatal(err)
		}
		writePair("old-")
		if err := m.copyTo(context.Background(), job, root); err != nil {
			t.Fatal(err)
		}
		writePair("new-")
		src, err := os.OpenRoot(source)
		if err != nil {
			t.Fatal(err)
		}
		defer src.Close()
		if err := syncTarget(src); err != nil {
			t.Fatal(err)
		}
		ctx := &afterPublication{Context: context.Background(), trigger: func() {
			got, _ := root.ReadFile(filepath.Join(live, "catalog"))
			if string(got) != "new-catalog" {
				return
			}
			if err := os.WriteFile("/tmp/reset-guest", nil, 0600); err != nil {
				t.Fatal(err)
			}
			time.Sleep(90 * time.Second)
			t.Fatal("guest reset did not happen")
		}}
		t.Fatal(m.copyTo(ctx, job, root))
	case "check":
		check(filepath.Join(recoveryFor(job, root).base, "previous", "docs"), "old-")
		if err := m.copyTo(context.Background(), job, root); err != nil {
			t.Fatal(err)
		}
		check(live, "new-")
	default:
		t.Fatal("invalid phase")
	}
}
