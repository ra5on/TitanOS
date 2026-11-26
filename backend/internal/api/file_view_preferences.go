package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"slices"
	"strings"
	"unicode"
)

// Separate from "files": desktop clients replace that entire object on save.
// Each edit here changes a single folder or the default under the account lock.
type viewPlace struct {
	Node string `json:"node,omitempty"`
	Root string `json:"root"`
	Path string `json:"path"`
}

type folderLayout struct {
	viewPlace
	View string `json:"view"`
}

type fileLayouts struct {
	Default string         `json:"default"`
	Folders []folderLayout `json:"folders"`
}

func validViewPlace(p viewPlace) bool {
	rootKind, rootID, _ := strings.Cut(p.Root, "/")
	validRoot := p.Root == "stick" || (rootID != "" && slices.Contains([]string{"share", "volume", "usb"}, rootKind))
	return validRoot && len(p.Node) <= 160 && len(p.Root) <= 4096 && len(p.Path) < 4096 &&
		strings.IndexFunc(p.Node+p.Root+p.Path, unicode.IsControl) < 0 &&
		(p.Path == "" || (!path.IsAbs(p.Path) && p.Path != "." && p.Path != ".." &&
			!strings.HasPrefix(p.Path, "../") && path.Clean(p.Path) == p.Path))
}

// Old installations have a view, sort order and direction in "files". Seed
// only the layout; the desktop keeps its original preferences and sort order.
func (p *preferences) initialFileViews(user string) (json.RawMessage, error) {
	stored, err := p.get(user, "files")
	if err != nil {
		return nil, err
	}
	var old struct {
		View    string
		Folders []folderLayout
	}
	_ = json.Unmarshal(stored, &old)
	legacy := func(v string) string {
		if v == "large" || v == "small" {
			return "grid"
		}
		return "list"
	}
	next := fileLayouts{Default: legacy(old.View), Folders: []folderLayout{}}
	seen := map[viewPlace]bool{}
	for _, f := range old.Folders {
		if len(next.Folders) >= 200 {
			break
		}
		if !validViewPlace(f.viewPlace) || seen[f.viewPlace] || !slices.Contains([]string{"list", "small", "large"}, f.View) {
			continue
		}
		seen[f.viewPlace] = true
		f.View = legacy(f.View)
		next.Folders = append(next.Folders, f)
	}
	return json.Marshal(next)
}

var errFolderViewsFull = errors.New("too many folder views")

func (a prefsArea) changeFileView(w http.ResponseWriter, r *http.Request, raw json.RawMessage) {
	var change struct {
		Folder *viewPlace `json:"folder"`
		View   string     `json:"view"`
	}
	if json.Unmarshal(raw, &change) != nil || (change.Folder != nil && !validViewPlace(*change.Folder)) ||
		!(slices.Contains([]string{"list", "details", "grid", "rail", "cards"}, change.View) || (change.Folder != nil && change.View == "inherit")) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid folder or view")
		return
	}
	user := currentUser(r).Name
	var result fileLayouts
	err := a.prefs.update(user, "fileviews", func(stored json.RawMessage) (json.RawMessage, error) {
		if stored == nil {
			var err error
			stored, err = a.prefs.initialFileViews(user)
			if err != nil {
				return nil, err
			}
		}
		if err := json.Unmarshal(stored, &result); err != nil {
			return nil, err
		}
		if change.Folder == nil {
			result.Default = change.View
		} else {
			index := slices.IndexFunc(result.Folders, func(f folderLayout) bool { return f.viewPlace == *change.Folder })
			switch {
			case change.View == "inherit":
				if index >= 0 {
					result.Folders = slices.Delete(result.Folders, index, index+1)
				}
			case index >= 0:
				result.Folders[index].View = change.View
			case len(result.Folders) >= 200:
				return nil, errFolderViewsFull
			default:
				result.Folders = append(result.Folders, folderLayout{*change.Folder, change.View})
			}
		}
		return json.Marshal(result)
	})
	if errors.Is(err, errFolderViewsFull) {
		writeError(w, http.StatusConflict, "bad_request", "200 folder layouts have already been saved")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the folder view could not be saved")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
