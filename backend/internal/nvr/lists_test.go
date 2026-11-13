package nvr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// A camera stored by 0.78 has no window, so no days: the API must still
// answer lists, never null (the camera form of 0.79 failed on it with
// "Beim Laden ist ein Fehler aufgetreten (500)", owner 13.11.2026).
func TestCamerasOf078AnswerLists(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "nvr"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, camerasFile), []byte("["+camera078+"]"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(config.Open(dir))
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	cams := m.Cameras()
	if len(cams) != 1 {
		t.Fatalf("cameras %d", len(cams))
	}
	data, _ := json.Marshal(cams[0])
	if strings.Contains(string(data), "null") && strings.Contains(string(data), `"days":null`) {
		t.Fatalf("days null: %s", data)
	}
	for _, field := range []string{`"days":[]`} {
		if !strings.Contains(string(data), field) {
			t.Errorf("missing %s in %s", field, data)
		}
	}
	if strings.Contains(string(data), `"kinds":null`) || strings.Contains(string(data), `"objects":null`) {
		t.Errorf("a list is null: %s", data)
	}
}
