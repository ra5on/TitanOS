package catalog

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestOfficialSourceMigrationAndDuplicateRejection(t *testing.T) {
	aliases := []string{OfficialURL, OfficialURL + "/", "https://github.com/RA5ON/titanos-apps.git", OfficialURL + "/archive/HEAD.tar.gz", OfficialURL + "/archive/refs/heads/main.zip"}
	for _, address := range aliases {
		t.Run(address, func(t *testing.T) {
			store := config.Open(t.TempDir())
			custom := Source{ID: "custom", Name: "TitanOS", URL: "https://example.org/custom.yaml"}
			input := struct {
				Sources []Source `json:"sources"`
			}{[]Source{{ID: "duplicate", Name: "TitanOS-Apps", URL: address}, custom}}
			if err := store.WriteJSON(sourcesFile, input); err != nil {
				t.Fatal(err)
			}
			if err := store.Write("apps/ollama/compose.yaml", []byte(plain)); err != nil {
				t.Fatal(err)
			}
			before := store.Changes()
			m := NewManager(store)
			if err := m.Load(); err != nil {
				t.Fatal(err)
			}
			sources, _ := m.Status()
			if len(sources) != 2 || sources[0].Name != "TitanOS-Apps" || sources[0].URL != OfficialURL || sources[1].Source != custom {
				t.Fatalf("sources: %+v", sources)
			}
			if store.Changes() != before+1 {
				t.Fatal("migration must write exactly once")
			}
			var saved struct{ Sources []Source }
			if err := store.ReadJSON(sourcesFile, &saved); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(saved.Sources, []Source{custom}) {
				t.Fatalf("saved: %+v", saved)
			}
			if err := m.Load(); err != nil {
				t.Fatal(err)
			}
			if store.Changes() != before+1 {
				t.Fatal("rewrote already migrated sources")
			}
			if err := m.Add(context.Background(), "Again", address); !errors.Is(err, ErrExists) {
				t.Fatalf("duplicate: %v", err)
			}
			data, err := store.Read("apps/ollama/compose.yaml")
			if err != nil || string(data) != plain {
				t.Fatal("installed app changed", err)
			}
		})
	}
}

func TestOtherCatalogSourcesRemainDistinct(t *testing.T) {
	for _, address := range []string{"https://github.com/other/TitanOS-Apps", OfficialURL + "/archive/refs/heads/testing.zip", OfficialURL + "/archive/refs/tags/v1.zip", OfficialURL + "/raw/main/ollama/compose.yaml", "https://github.com.evil.invalid/ra5on/TitanOS-Apps"} {
		if sameSource(address, OfficialURL) {
			t.Fatalf("unrelated source matched: %s", address)
		}
	}
}

func TestCustomBranchNotCollapsedIntoRepository(t *testing.T) {
	if sameSource("https://github.com/example/apps/archive/refs/heads/main.zip", "https://github.com/example/apps") {
		t.Fatal("custom branch collapsed into potentially different default branch")
	}
}
