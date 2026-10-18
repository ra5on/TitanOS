package backup

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSMBValidationAndSecrets(t *testing.T) {
	target := SMBTarget{ID: "second-nas", Host: "nas.example", Username: "backup", Password: "secret,=value", Share: "My backups", Folder: "titanos"}
	if err := validateSMB(target, true); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*SMBTarget){func(t *SMBTarget) { t.Host = "-option" }, func(t *SMBTarget) { t.Password = "x\nusername=root" }, func(t *SMBTarget) { t.Share = "../data" }, func(t *SMBTarget) { t.Folder = "../outside" }, func(t *SMBTarget) { t.Username = "x\npassword=x" }} {
		bad := target
		change(&bad)
		if validateSMB(bad, true) == nil {
			t.Fatal("unsafe target accepted")
		}
	}
	store := config.Open(t.TempDir())
	m := NewManager(store)
	m.workDir = t.TempDir()
	if err := store.WriteJSON(smbTargetsFile, []SMBTarget{target}); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	shown, _ := json.Marshal(m.SMBTargets())
	if strings.Contains(string(shown), target.Password) || strings.Contains(string(shown), "password") {
		t.Fatal("secret exposed")
	}
	redacted := target
	redacted.Password = ""
	if m.smbCredentials(redacted).Password != target.Password {
		t.Fatal("stored password not reused")
	}
	redacted.Host = "other.example"
	if m.smbCredentials(redacted).Password != "" {
		t.Fatal("password sent to new server")
	}
	dir, err := m.smbAuth(target)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	info, err := os.Stat(filepath.Join(dir, "credentials"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential permissions", err)
	}
	m.Disabled = true
	if _, err := m.SMBDiscover(context.Background(), target); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	if err := m.SaveSMBTarget(context.Background(), target); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	if err := remoteSMB(context.Background(), "127.0.0.1"); err == nil {
		t.Fatal("self target accepted")
	}
}
func TestSMBJobsAndReferencedTarget(t *testing.T) {
	m := NewManager(config.Open(t.TempDir()))
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	m.Sources = func() []Source { return []Source{{Name: "docs", Volume: "data", Path: t.TempDir()}} }
	job := Job{ID: "copy", Sources: []string{"docs"}, Time: "02:00", Destination: Destination{Type: "smb-copy", TargetID: "nas"}}
	if m.Create(job, nil) == nil {
		t.Fatal("nonexistent target accepted")
	}
	m.smbTargets = []SMBTarget{{ID: "nas"}}
	if err := m.Create(job, nil); err != nil {
		t.Fatal(err)
	}
	if m.DeleteSMBTarget("nas") == nil {
		t.Fatal("referenced target removed")
	}
	saved, _ := m.find("copy")
	if saved.Password != "" || saved.Destination.Folder != "backups/copy" {
		t.Fatal(saved)
	}
	if _, _, _, _, err := m.repository(saved); err == nil {
		t.Fatal("copy treated as restic")
	}
	job.Extras = []string{"config"}
	if m.Update("copy", job, nil) == nil {
		t.Fatal("plaintext credentials could be copied")
	}
}
func TestCopySkipsIdenticalButDetectsSameSizeAndTimestampChanges(t *testing.T) {
	sourceDir, targetDir := t.TempDir(), t.TempDir()
	src, _ := os.OpenRoot(sourceDir)
	defer src.Close()
	dst, _ := os.OpenRoot(targetDir)
	defer dst.Close()
	os.WriteFile(filepath.Join(sourceDir, "data"), []byte("first"), 0600)
	if _, err := copyFile(context.Background(), src, "data", dst, "data"); err != nil {
		t.Fatal(err)
	}
	before, _ := dst.Stat("data")
	if _, err := copyFile(context.Background(), src, "data", dst, "data"); err != nil {
		t.Fatal(err)
	}
	after, _ := dst.Stat("data")
	if !os.SameFile(before, after) {
		t.Fatal("identical destination rewritten")
	}
	os.WriteFile(filepath.Join(sourceDir, "data"), []byte("other"), 0600)
	src.Chtimes("data", before.ModTime(), before.ModTime())
	if _, err := copyFile(context.Background(), src, "data", dst, "data"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(targetDir, "data"))
	if string(data) != "other" {
		t.Fatal("same-size/timestamp edit lost")
	}
}
