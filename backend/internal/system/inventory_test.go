package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInventoryMissingAndPlaceholder(t *testing.T) {
	dir := t.TempDir()
	if got := readInventory(dir); got != (Inventory{}) {
		t.Fatal(got)
	}
	for k, v := range map[string]string{"board_vendor": "Default string\n", "board_name": "Board 42\n", "bios_version": "v1.2\n", "bios_date": "10/24/2026\n"} {
		if err := os.WriteFile(filepath.Join(dir, k), []byte(v), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got := readInventory(dir)
	if got.Mainboard != "Board 42" || got.BIOS != "v1.2 · 10/24/2026" {
		t.Fatal(got)
	}
}
