package files

import "testing"

// Show lists all a root shows; everything else is a secret.
func TestRootShowIsAPositiveList(t *testing.T) {
	root := Root{Hide: []string{"users"}, Show: []string{"apps/", "system/settings.json"}}
	for rel, want := range map[string]bool{
		".": false, "": false, "apps": false, "apps/web/compose.yaml": false, "apps/web/.env": true,
		"system": false, "system/settings.json": false, "system/settings.json.prev": false,
		"system/ups.json": true, "homeassistant": true, "homeassistant/settings.json": true,
		"users": true, "users/alex/account.json": true, "appsx": true, "system/settings.jsonx": true,
	} {
		if got := root.secret(rel); got != want {
			t.Errorf("secret(%q) = %v, want %v", rel, got, want)
		}
	}
}
