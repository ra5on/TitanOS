package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"testing"
)

// A mobile edit neither changes Explorer's preferences nor another root,
// node or account. After a reset the folder follows future default changes.
func TestMobileFileViews(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	legacy := map[string]any{"view": "list", "sort": "size", "descending": true, "folders": []map[string]any{{"root": "share/data", "path": "Medien", "view": "large", "sort": "modified", "descending": false}}}
	if res, _ := ts.request("PUT", "/api/v1/preferences/files", legacy); res.StatusCode != http.StatusNoContent {
		t.Fatal(res.StatusCode)
	}
	get := func() fileLayouts {
		t.Helper()
		res, body := ts.request("GET", "/api/v1/preferences", nil)
		var prefs struct{ Fileviews fileLayouts }
		if res.StatusCode != http.StatusOK || json.Unmarshal(body, &prefs) != nil {
			t.Fatalf("get: %d %s", res.StatusCode, body)
		}
		return prefs.Fileviews
	}
	local := viewPlace{Root: "share/data", Path: "Medien"}
	if got := get(); got.Default != "list" || !reflect.DeepEqual(got.Folders, []folderLayout{{local, "grid"}}) {
		t.Fatalf("legacy: %+v", got)
	}
	edit := func(view string, folder *viewPlace) {
		t.Helper()
		res, body := ts.request("PUT", "/api/v1/preferences/fileviews", map[string]any{"folder": folder, "view": view})
		if res.StatusCode != http.StatusOK {
			t.Fatalf("edit: %d %s", res.StatusCode, body)
		}
	}
	other := viewPlace{Root: "volume/Data", Path: "Medien"}
	peer := viewPlace{Node: "peer", Root: "share/data", Path: "Medien"}
	edit("cards", &peer)
	edit("details", &other)
	edit("rail", nil)
	edit("inherit", &local)
	edit("list", nil)
	got := get()
	if got.Default != "list" || !reflect.DeepEqual(got.Folders, []folderLayout{{peer, "cards"}, {other, "details"}}) {
		t.Fatalf("reset and defaults: %+v", got)
	}
	reloaded := newPreferences(ts.store)
	stored, err := reloaded.get("alex", "fileviews")
	var durable fileLayouts
	if err != nil || json.Unmarshal(stored, &durable) != nil || !reflect.DeepEqual(durable, got) {
		t.Fatalf("reopened: %s %v", stored, err)
	}
	files, err := reloaded.get("alex", "files")
	var old map[string]any
	_ = json.Unmarshal(files, &old)
	expected, _ := json.Marshal(legacy)
	var want map[string]any
	_ = json.Unmarshal(expected, &want)
	if err != nil || !reflect.DeepEqual(old, want) {
		t.Fatal("desktop preferences changed")
	}
	fresh, err := reloaded.initialFileViews("another-user")
	if err != nil || string(fresh) != `{"default":"list","folders":[]}` {
		t.Fatalf("other user: %s %v", fresh, err)
	}
	if res, _ := ts.do(ts.newClient(), "PUT", "/api/v1/preferences/fileviews", map[string]any{"view": "grid"}); res.StatusCode != http.StatusUnauthorized {
		t.Fatal("unauthenticated write")
	}
	for _, change := range []any{map[string]any{"view": "inherit"}, map[string]any{"view": "unknown"}, map[string]any{"folder": viewPlace{Root: "share/data", Path: "../etc"}, "view": "grid"}} {
		if res, _ := ts.request("PUT", "/api/v1/preferences/fileviews", change); res.StatusCode != http.StatusBadRequest {
			t.Fatal("invalid change accepted", change)
		}
	}
}

func TestMobileFileViewsCapacity(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	folders := make([]folderLayout, 200)
	for i := range folders {
		folders[i] = folderLayout{viewPlace{Root: "share/data", Path: strconv.Itoa(i)}, "list"}
	}
	if res, body := ts.request("PUT", "/api/v1/preferences/files", map[string]any{"folders": folders}); res.StatusCode != http.StatusNoContent {
		t.Fatal(string(body))
	}
	edit := func(path, view string, want int) {
		t.Helper()
		res, body := ts.request("PUT", "/api/v1/preferences/fileviews", map[string]any{"folder": viewPlace{Root: "share/data", Path: path}, "view": view})
		if res.StatusCode != want {
			t.Fatalf("%s: %d %s", path, res.StatusCode, body)
		}
	}
	edit("additional", "grid", http.StatusConflict)
	edit("17", "cards", http.StatusOK)
	edit("17", "inherit", http.StatusOK)
	edit("additional", "grid", http.StatusOK)
}
