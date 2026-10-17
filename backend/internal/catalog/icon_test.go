package catalog

import "testing"

func TestCatalogIcons(t *testing.T) {
	for _, value := range []string{"javascript:alert(1)", "http://example.org/i.png", "//example.org/i.png", "https://user:pass@example.org/i.png", "https://"} {
		if got := safeIcon(value); got != "" {
			t.Errorf("accepted unsafe icon %q", value)
		}
	}
	apps, _, err := parse("test", map[string]string{"app/compose.yaml": "x-titanos:\n  name: App\n  icon: https://example.org/icon.png\nservices:\n  app:\n    image: example:v1\n"})
	if err != nil || len(apps) != 1 || apps[0].Icon != "https://example.org/icon.png" {
		t.Fatalf("icon lost: %+v %v", apps, err)
	}
}
