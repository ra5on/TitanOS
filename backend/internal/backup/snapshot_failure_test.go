package backup

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPreviousSnapshotRestoresAfterFailure(t *testing.T) {
	binary := os.Getenv("RESTIC_TEST_BINARY")
	if binary == "" {
		t.Skip("set RESTIC_TEST_BINARY to restic with 'restore --overwrite never'")
	}
	for _, fault := range []string{"cancel", "full"} {
		t.Run(fault, func(t *testing.T) {
			e := newEnv(t)
			e.m.restic = binary
			e.mount(t, "cold")
			e.mount(t, "tank")
			cold := filepath.Join(e.m.mountRoot, "cold")
			if fault == "full" {
				cmdline, _ := os.ReadFile("/proc/cmdline")
				if !strings.Contains(" "+strings.TrimSpace(string(cmdline))+" ", " titanos.move-test=1 ") {
					t.Skip("disk-full test requires the disposable QEMU guest")
				}
				// Only the fresh 128 MiB scratch image in the test VM is filled.
				if err := syscall.Mount("/move-qa/backup", cold, "", syscall.MS_BIND, ""); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := syscall.Unmount(cold, 0); err != nil {
						t.Error(err)
					}
				})
			}
			job := s3Job()
			job.Keep = Keep{Last: 1}
			job.Destination = Destination{Type: "local", Volume: "cold", Folder: "repo"}
			src := filepath.Join(e.m.sources()[0].Path, "family")
			if err := os.Mkdir(src, 0750); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(src, "private.txt")
			want := "previous complete snapshot\n"
			if err := os.WriteFile(file, []byte(want), 0640); err != nil {
				t.Fatal(err)
			}
			beforeDir, err := os.Stat(src)
			if err != nil {
				t.Fatal(err)
			}
			beforeFile, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.m.Create(job, volumes); err != nil {
				t.Fatal(err)
			}
			start := func() {
				t.Helper()
				if err := e.m.Start(job.ID); err != nil {
					t.Fatal(err)
				}
			}
			start()
			e.m.runs.Wait()
			if st := e.state(job.ID); st.LastError != "" || st.LastOK == nil {
				t.Fatal(st)
			}
			old, err := e.m.Snapshots(context.Background(), job.ID)
			if err != nil || len(old) != 1 {
				t.Fatal(old, err)
			}
			if err := os.WriteFile(file, []byte("new, unsaved content"), 0600); err != nil {
				t.Fatal(err)
			}
			extra, err := os.Create(filepath.Join(src, "new.bin"))
			if err != nil {
				t.Fatal(err)
			}
			size := int64(20 << 20)
			if fault == "cancel" {
				size = 64 << 20 // several independent restic packs
			}
			_, writeErr := io.CopyN(extra, rand.Reader, size)
			if err := errors.Join(writeErr, extra.Close()); err != nil {
				t.Fatal(err)
			}
			if fault == "full" {
				filler, err := os.Create(filepath.Join(cold, "ballast"))
				if err != nil {
					t.Fatal(err)
				}
				defer filler.Close()
				// Grow in small extents, accounting for ext4 metadata and reserved
				// blocks. Leave room for restic's lock, but not another data pack.
				var offset int64
				for ; offset < 128<<20; offset += 512 << 10 {
					err := syscall.Fallocate(int(filler.Fd()), 0, offset, 512<<10)
					if errors.Is(err, syscall.ENOSPC) {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if offset < 1<<20 || offset >= 128<<20 {
					t.Fatalf("unexpected disposable disk capacity: %d", offset)
				}
				if err := filler.Truncate(offset - 256<<10); err != nil {
					t.Fatal(err)
				}
				if err := filler.Close(); err != nil {
					t.Fatal(err)
				}
			}
			start()
			if fault == "cancel" {
				// The original snapshot contains only short text. A large, published
				// pack proves the update has changed the repository before Stop.
				wrotePack := false
				deadline := time.Now().Add(2 * time.Minute)
				for e.state(job.ID).Running && time.Now().Before(deadline) && !wrotePack {
					files, err := filepath.Glob(filepath.Join(cold, "repo", "data", "*", "*"))
					if err != nil {
						t.Fatal(err)
					}
					for _, file := range files {
						info, err := os.Stat(file)
						if err == nil && len(info.Name()) == 64 && info.Size() > 1<<20 {
							t.Logf("Published %d-byte restic data pack before Stop", info.Size())
							wrotePack = true
							break
						}
					}
					if !wrotePack {
						time.Sleep(10 * time.Millisecond)
					}
				}
				err := e.m.Stop(job.ID)
				if !wrotePack || err != nil {
					e.m.runs.Wait()
					t.Fatalf("cancel after first published data pack: found=%t, stop=%v", wrotePack, err)
				}
			}
			e.m.runs.Wait()
			state := e.state(job.ID)
			if fault == "cancel" && state.LastError != errCancelled || fault == "full" && state.LastErrorCode != "backup_no_space" {
				t.Fatalf("failure reported incorrectly: %+v", state)
			}
			if fault == "full" {
				if err := os.Remove(filepath.Join(cold, "ballast")); err != nil {
					t.Fatal(err)
				}
			}
			snapshots, err := e.m.Snapshots(context.Background(), job.ID)
			if err != nil || len(snapshots) != 1 || snapshots[0].ID != old[0].ID {
				t.Fatalf("changed snapshot set: %v %v", snapshots, err)
			}
			target, err := e.m.Restore(job.ID, old[0].ID, "tank")
			if err != nil {
				t.Fatal(err)
			}
			e.m.runs.Wait()
			if st := e.state(job.ID); st.RestoredTo != target || st.RestoreError != "" {
				t.Fatal(st)
			}
			restored := filepath.Join(target, file)
			content, err := os.ReadFile(restored)
			if err != nil || string(content) != want {
				t.Fatalf("restored bytes %q, %v", content, err)
			}
			for name, before := range map[string]os.FileInfo{restored: beforeFile, filepath.Dir(restored): beforeDir} {
				after, err := os.Stat(name)
				if err != nil {
					t.Fatal(err)
				}
				a, b := after.Sys().(*syscall.Stat_t), before.Sys().(*syscall.Stat_t)
				if before.Mode() != after.Mode() || a.Uid != b.Uid || a.Gid != b.Gid {
					t.Fatalf("mode or owner changed: %s", name)
				}
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(restored), "new.bin")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unexpected file in restore", err)
			}
		})
	}
}
