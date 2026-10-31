package configbackup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An encrypted backup lists like any other, gives away no secret, and
// comes back only with its password.
func TestEncryptedBackup(t *testing.T) {
	e := newEnv(t)
	e.write(t, "notify/settings.json", `{"token":"very-secret-token"}`)
	if err := e.m.SetPassword("short"); !errors.Is(err, ErrInvalid) {
		t.Fatal("short password accepted")
	}
	if err := e.m.SetPassword("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	settings := e.m.current()
	settings.Encrypt = true
	if err := e.m.SetSettings(settings, []string{"tank", "fast"}); err != nil {
		t.Fatal(err)
	}
	name := e.create(t)
	data, err := os.ReadFile(filepath.Join(e.root, "mnt", "fast", folder, name))
	if err != nil {
		t.Fatal(err)
	}
	if !encrypted(data) || strings.Contains(string(data), "very-secret-token") {
		t.Fatal("backup not sealed")
	}
	list := readFolder(filepath.Join(e.root, "mnt", "fast", folder))
	if len(list) != 1 || !list[0].Encrypted || list[0].Version != "1.2.3" {
		t.Fatalf("list: %+v", list)
	}
	stick := newStick(t, e)
	if _, err := stick.m.RestoreFound(context.Background(), "md127", name, ""); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("no password: %v", err)
	}
	if _, err := stick.m.RestoreFound(context.Background(), "md127", name, "wrong password"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := stick.m.RestoreFound(context.Background(), "md127", name, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if got, _ := stick.store.Read("notify/settings.json"); !strings.Contains(string(got), "very-secret-token") {
		t.Fatalf("restored: %s", got)
	}
}

func TestEncryptWithoutPasswordRefusesToSave(t *testing.T) {
	e := newEnv(t)
	settings := e.m.current()
	settings.Encrypt = true
	e.m.SetSettings(settings, []string{"tank"})
	if _, err := e.m.Create([]string{"tank"}); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("saved unencrypted: %v", err)
	}
}
