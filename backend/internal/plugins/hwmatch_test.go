package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestPluginsFor(t *testing.T) {
	table := t.TempDir()
	os.WriteFile(filepath.Join(table, "wangxun-nic.json"), []byte(`{"title":"Wangxun","hardware":[{"modalias":"pci:v00008088d00001001sv*sd*bc*sc*i*","unbound":true}]}`), 0o644)
	os.WriteFile(filepath.Join(table, "coral-usb.json"), []byte(`{"title":"Coral","hardware":[{"modalias":"usb:v1A6Ep089A*"}]}`), 0o644)
	m := &Manager{store: config.Open(t.TempDir()), catalogTable: table, active: map[string]Manifest{}, resident: map[string]Manifest{}, problems: map[string]string{}, applied: "-"}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	nic := "pci:v00008088d00001001sv00008088sd00000000bc02sc00i00"
	coral := "usb:v1A6Ep089Ad0100dc00dsc00dp00icFFisc00ip00in00"
	other := "pci:v000010ECd00008127sv00001849sd00008127bc02sc00i00"
	got := m.PluginsFor([]string{nic, coral, other, ""}, nil)
	if len(got[nic]) != 1 || got[nic][0].Plugin != "wangxun-nic" || got[nic][0].Title != "Wangxun" || got[nic][0].Installed {
		t.Errorf("nic: %+v", got[nic])
	}
	if len(got[coral]) != 1 || got[coral][0].Plugin != "coral-usb" {
		t.Errorf("coral: %+v", got[coral])
	}
	if len(got[other]) != 0 || len(got) != 2 {
		t.Errorf("unmatched: %+v", got)
	}
	// A running plugin action does not block the lookup.
	m.actions.Lock()
	defer m.actions.Unlock()
	if got := m.PluginsFor([]string{nic}, nil); len(got[nic]) != 1 {
		t.Errorf("during an action: %+v", got)
	}
}
