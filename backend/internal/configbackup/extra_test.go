package configbackup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtraFilesAreSavedAndRestored(t *testing.T) {
	e := newEnv(t)
	extra := []ExtraFile{
		{Name: ExtraPrefix + "nvr/faces/Anna/a.webp", Data: []byte("face-a")},
		{Name: ExtraPrefix + "nvr/faces/Ben/b.webp", Data: []byte("face-b")},
		{Name: "users/evil/account.json", Data: []byte("{}")},       // not below the prefix
		{Name: ExtraPrefix + "../users/x.json", Data: []byte("{}")}, // leaves it
	}
	e.m.Extra = func() []ExtraFile { return extra }
	first := e.create(t)
	// The same extra files: no new backup. A changed one: a new backup.
	e.clock = e.clock.Add(time.Hour)
	if again := e.create(t); again != first {
		t.Errorf("unchanged extra files saved again")
	}
	extra[0].Data = []byte("face-a2")
	second := e.create(t)
	if second == first {
		t.Fatal("changed extra file not saved")
	}
	// Restoring on a new stick puts them below the same names.
	stick := newEnv(t)
	os.RemoveAll(filepath.Join(stick.root, "config"))
	file, err := os.Open(filepath.Join(e.root, "mnt/fast", folder, second))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := stick.m.RestoreUpload(file, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := stick.store.Read(ExtraPrefix + "nvr/faces/Anna/a.webp"); err != nil || string(data) != "face-a2" {
		t.Errorf("restored: %q %v", data, err)
	}
	if _, err := stick.store.Read("users/evil/account.json"); err == nil {
		t.Error("an extra file outside the prefix was saved")
	}
	// What waits on the stick after a restore is not saved as
	// configuration next to what Extra returns: no name twice.
	stick.m.Extra = func() []ExtraFile { return extra[:2] }
	name, err := stick.m.Create([]string{"tank"})
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := os.Open(filepath.Join(stick.root, "mnt/tank", folder, name))
	defer saved.Close()
	if _, err := check(saved); err != nil {
		t.Errorf("backup with waiting extra files: %v", err)
	}
	// Too much is left out as a whole and never fails the backup.
	e.m.Extra = func() []ExtraFile {
		return []ExtraFile{{Name: ExtraPrefix + "nvr/big", Data: []byte(strings.Repeat("x", int(maxExtra)+1))}}
	}
	if _, err := e.m.Create([]string{"tank", "fast"}); err != nil {
		t.Errorf("oversized extra files broke the backup: %v", err)
	}
}
