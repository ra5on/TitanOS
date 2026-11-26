package config

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// The harness kills this VM once the real store has returned success. The
// host inspects the FAT image before another boot or fsck can modify it.
func TestGuestFATStorePowerCut(t *testing.T) {
	cmdline, _ := os.ReadFile("/proc/cmdline")
	if !strings.Contains(" "+strings.TrimSpace(string(cmdline))+" ", " titanos.fat-store-test=1 ") {
		t.Skip("requires the disposable FAT guest")
	}
	store := Open("/stick/config")
	payload := struct {
		Volume     string `json:"volume"`
		Generation int    `json:"generation"`
		Padding    string `json:"padding"`
	}{"testdaten", 2, strings.Repeat("x", 280)}
	file := "system/agent.json"
	if strings.Contains(string(cmdline), "titanos.nested=1") {
		file = "system/new/node/agent.json"
	}
	if err := store.WriteJSON(file, payload); err != nil {
		t.Fatal(err)
	}
	wanted, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Read(file)
	if err != nil || !bytes.Equal(got, append(wanted, '\n')) {
		t.Fatal("read-back before reset differed", err)
	}
	if strings.Contains(string(cmdline), "titanos.update-part=1") {
		f, err := os.Create("/stick/titanos/.incoming-b/filesystem.squashfs.part")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(make([]byte, 8<<20)); err != nil {
			t.Fatal(err)
		}
		if err := f.Sync(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	println("TITANOS_FAT_STORE_ACKNOWLEDGED")
	time.Sleep(90 * time.Second)
	t.Fatal("power cut did not occur")
}
