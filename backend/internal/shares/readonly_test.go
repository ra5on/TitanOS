package shares

import (
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestReadOnlyExportAndLegacyRollback(t *testing.T) {
	m, _ := newTestManager(t)
	s := Share{Name: "documents", Volume: "data", SMB: true, NFS: NFS{Mode: ReadOnly, Clients: []string{"192.168.20.0/24"}}, Access: map[string]string{"alex": ReadWrite}}
	if err := m.Create(s, volumes, users); err != nil {
		t.Fatal(err)
	}
	runtime := m.List()[0]
	if !runtime.NFS.Enabled || runtime.Access["alex"] != ReadWrite {
		t.Fatal("NFS state or independent SMB permissions lost")
	}
	exports := renderExports([]Share{runtime}, "/srv/nfs")
	if strings.Contains(exports, "(rw,") || !strings.Contains(exports, "/documents 192.168.20.0/24(ro,") {
		t.Fatal(exports)
	}
	// A previous application only understands enabled/clients.
	var old []struct {
		NFS struct {
			Enabled bool `json:"enabled"`
		} `json:"nfs"`
	}
	if err := m.store.ReadJSON(configFile, &old); err != nil {
		t.Fatal(err)
	}
	if old[0].NFS.Enabled {
		t.Fatal("old application would export protected data writable")
	}
	restarted := NewManager(config.Open(m.store.Dir()))
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	if !restarted.List()[0].NFS.Enabled || restarted.List()[0].NFS.Mode != ReadOnly {
		t.Fatal("protected export not restored on restart")
	}
	s.NFS.Mode = "off"
	if err := restarted.Update(s.Name, s, volumes, users); err != nil {
		t.Fatal(err)
	}
	if restarted.List()[0].NFS.Enabled {
		t.Fatal("explicit disable ignored")
	}
}
