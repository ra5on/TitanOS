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

// A root handed to a token, an MCP client or the AI agent hides what TitanOS
// keeps on the volume and every .env file, on any level.
func TestMachineRootHidesInternalFiles(t *testing.T) {
	machine, person := Root{Machine: true}, Root{}
	for rel, want := range map[string]bool{
		".titanos": true, ".titanos/docker/app/.env": true, "Docs/.TitanOS/x": true,
		"app/.env": true, "app/prod.env": true, "up/film.mkv.titanos-part": true,
		"Docs/readme.txt": false, "Docs/environment.txt": false, "": false,
	} {
		if got := machine.secret(rel); got != want {
			t.Errorf("machine root: secret(%q) = %v, want %v", rel, got, want)
		}
		if person.secret(rel) {
			t.Errorf("a person in the web interface must still see %q", rel)
		}
	}
}
