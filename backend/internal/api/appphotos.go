package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appphotos"
	"github.com/ra5on/TitanOS/backend/internal/files"
)

// The photo backup of the Android app (PLAN 116, TECHNIK §3,
// docs/ANDROID-APP.md "Foto-Sicherung"). Only the session of a paired app
// reaches these routes: they are neither in publicRoutes nor in the tool
// catalog, and a browser session without a device is refused. Rights are
// always those of the account on the share right now (fileRoot); the index
// only says what the server itself finished and hashed, never what the app
// claims.

const (
	// photoItemsMax bounds the entries of one check or verify request.
	photoItemsMax = 500
	// photoBodyMax bounds its body.
	photoBodyMax = 1 << 20
	// photoHashBudget is how many bytes one request reads again to be sure
	// (a file whose time changed, a file already in the target that the
	// index does not know); what does not fit is "unsure".
	photoHashBudget = 1 << 30
)

// Answers of check and verify.
const (
	photoSaved   = "saved"
	photoMissing = "missing"
	photoUnsure  = "unsure"
)

type appPhotosArea struct {
	appSessionGuard
	files filesArea
	store *appphotos.Store
}

func (s *Server) appPhotosArea() appPhotosArea {
	return appPhotosArea{appSessionGuard: s.appSessionGuard(), files: s.filesArea(), store: s.opts.AppPhotos}
}

// appSessionGuard lets only the session of a paired app through (photo
// backup, messages).
type appSessionGuard struct {
	sessions sessionsFunc
	token    func(*http.Request) string
}

func (s *Server) appSessionGuard() appSessionGuard {
	return appSessionGuard{sessions: s.sessionStore, token: s.authArea().sessionToken}
}

func (s *Server) registerAppPhotos(mux *routeMux) {
	a, g := s.appPhotosArea(), s.guards()
	app := func(h http.HandlerFunc) http.HandlerFunc { return g.user(g.clusterLocal(g.browserOnly(a.appOnly(h)))) }
	mux.HandleFunc("POST /api/v1/app/photos/check", app(a.handleCheck))
	mux.HandleFunc("POST /api/v1/app/photos/verify", app(a.handleVerify))
	mux.HandleFunc("POST /api/v1/app/photos/uploads/{id}/finish", app(a.handleFinish))
}

// device is the paired app behind the request's session, "" for any other.
func (a appSessionGuard) device(r *http.Request) string {
	if a.sessions == nil {
		return ""
	}
	session, ok := a.sessions().Get(a.token(r))
	if !ok || session.User != currentUser(r).Name {
		return ""
	}
	return session.Device
}

func (a appSessionGuard) appOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.device(r) == "" {
			writeError(w, http.StatusForbidden, "app_session_required", "only the paired Android app can do this")
			return
		}
		next(w, r)
	}
}

func writePhotosError(w http.ResponseWriter, err error) {
	if errors.Is(err, appphotos.ErrUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "photos_unavailable", "the photo backup needs the system data volume, which is not there")
		return
	}
	slog.Error("photo backup", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "the photo backup failed")
}

type photoItem struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// Path is where the app would put the file, relative to dir (check).
	Path string `json:"path,omitempty"`
}

type photoAnswer struct {
	SHA256 string `json:"sha256"`
	State  string `json:"state"`
	Root   string `json:"root,omitempty"`
	Path   string `json:"path,omitempty"`
}

func readPhotoItems(w http.ResponseWriter, r *http.Request, v any, items func() []photoItem) bool {
	r.Body = http.MaxBytesReader(w, r.Body, photoBodyMax)
	if !readJSONBody(w, r, v) {
		return false
	}
	list := items()
	if len(list) > photoItemsMax {
		writeError(w, http.StatusBadRequest, "photos_too_many", "at most 500 entries per request")
		return false
	}
	for _, it := range list {
		if !appphotos.ValidSum(it.SHA256) || it.Size < 0 {
			writeError(w, http.StatusBadRequest, "invalid_checksum", "every entry needs a SHA-256 as 64 hex digits and a size")
			return false
		}
	}
	return true
}

// within reports whether p lies in dir or below it.
func within(dir, p string) bool {
	return dir == "." || p == dir || strings.HasPrefix(p, dir+"/")
}

// fileState is a regular file in a root, opened without following links.
type fileState struct {
	size    int64
	modTime int64
}

func statInRoot(root files.Root, p string) (fileState, error) {
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return fileState{}, err
	}
	defer r.Close()
	st, err := r.Lstat(p)
	if err != nil {
		return fileState{}, err
	}
	if !st.Mode().IsRegular() {
		return fileState{}, fs.ErrNotExist
	}
	return fileState{size: st.Size(), modTime: st.ModTime().UnixNano()}, nil
}

// hashInRoot reads a file of a root for its SHA-256 and takes it from the
// budget; ok is false once the budget would run out.
func hashInRoot(root files.Root, p string, budget *int64) (sum string, state fileState, ok bool, err error) {
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return "", fileState{}, false, err
	}
	defer r.Close()
	f, err := r.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fileState{}, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", fileState{}, false, err
	}
	if !st.Mode().IsRegular() {
		return "", fileState{}, false, fs.ErrNotExist
	}
	if st.Size() > *budget {
		return "", fileState{}, false, nil
	}
	*budget -= st.Size()
	h := sha256.New()
	if _, err := io.CopyBuffer(h, f, make([]byte, 64<<10)); err != nil {
		return "", fileState{}, false, err
	}
	return hex.EncodeToString(h.Sum(nil)), fileState{size: st.Size(), modTime: st.ModTime().UnixNano()}, true, nil
}

// handleCheck answers which pictures already lie in a target (share and
// folder, subfolders included): found by their content in the index of any
// device of the account, so a renamed picture or a phone paired anew is
// not uploaded twice. A picture the index does not know but whose path the
// app names is read once and taken into the index if it is the same.
func (a appPhotosArea) handleCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root  string      `json:"root"`
		Dir   string      `json:"dir"`
		Items []photoItem `json:"items"`
	}
	if !readPhotoItems(w, r, &req, func() []photoItem { return req.Items }) {
		return
	}
	dir, err := files.CleanPath(req.Dir)
	if err != nil {
		writeFileError(w, files.ErrInvalid)
		return
	}
	root, ok := a.files.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	user, device := currentUser(r).Name, a.device(r)
	index, err := a.store.Lookup(user)
	if err != nil {
		writePhotosError(w, err)
		return
	}
	budget := int64(photoHashBudget)
	out := make([]photoAnswer, 0, len(req.Items))
	for _, it := range req.Items {
		answer := photoAnswer{SHA256: it.SHA256, State: photoMissing}
		for _, e := range index[it.SHA256] {
			if e.Root != root.ID || e.Size != it.Size || !within(dir, e.Path) {
				continue
			}
			if st, err := statInRoot(root, e.Path); err == nil && st.size == e.Size && st.modTime == e.ModTime {
				answer.State, answer.Root, answer.Path = photoSaved, root.ID, e.Path
				break
			}
		}
		if answer.State != photoSaved && it.Path != "" {
			answer.State = a.probe(root, dir, it, user, device, &budget, &answer)
		}
		out = append(out, answer)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// probe looks at the place the app would put a picture: the same content
// there counts as saved (and goes into the index), another or none as
// missing.
func (a appPhotosArea) probe(root files.Root, dir string, it photoItem, user, device string, budget *int64, answer *photoAnswer) string {
	p, err := files.CleanPath(path.Join(dir, it.Path))
	if err != nil || p == "." || !within(dir, p) {
		return photoMissing
	}
	st, err := statInRoot(root, p)
	if err != nil || st.size != it.Size {
		return photoMissing
	}
	sum, state, ok, err := hashInRoot(root, p, budget)
	switch {
	case err != nil:
		return photoMissing
	case !ok:
		return photoUnsure
	case sum != it.SHA256:
		return photoMissing
	}
	if err := a.store.Record(user, device, appphotos.Entry{SHA256: sum, Size: state.size, Root: root.ID, Path: p, ModTime: state.modTime}); err != nil {
		slog.Warn("photo backup: record a found picture", "err", err)
	}
	answer.Root, answer.Path = root.ID, p
	return photoSaved
}

// handleVerify confirms before "Speicher freigeben" that a picture lies on
// the NAS with its content: in the index, in a root the account can still
// reach, a regular file of the same size and time. A changed time is read
// again within the budget; what cannot be checked now is "unsure", never
// confirmed.
func (a appPhotosArea) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []photoItem `json:"items"`
	}
	if !readPhotoItems(w, r, &req, func() []photoItem { return req.Items }) {
		return
	}
	user, device := currentUser(r).Name, a.device(r)
	index, err := a.store.Lookup(user)
	if err != nil {
		writePhotosError(w, err)
		return
	}
	roots := map[string]files.Root{}
	for _, root := range a.files.requestRoots(r) {
		roots[root.ID] = root
	}
	budget := int64(photoHashBudget)
	out := make([]photoAnswer, 0, len(req.Items))
	for _, it := range req.Items {
		answer := photoAnswer{SHA256: it.SHA256, State: photoMissing}
		for _, e := range index[it.SHA256] {
			root, ok := roots[e.Root]
			if !ok || e.Size != it.Size {
				continue
			}
			st, err := statInRoot(root, e.Path)
			if err != nil || st.size != e.Size {
				continue
			}
			if st.modTime != e.ModTime {
				sum, state, ok, err := hashInRoot(root, e.Path, &budget)
				if err != nil || (ok && sum != e.SHA256) {
					continue
				}
				if !ok {
					answer.State = photoUnsure
					continue
				}
				// Same content, new time (copied back, touched): kept anew.
				if err := a.store.Record(user, device, appphotos.Entry{SHA256: sum, Size: state.size, Root: e.Root, Path: e.Path, ModTime: state.modTime}); err != nil {
					slog.Warn("photo backup: record a checked picture", "err", err)
				}
			}
			answer = photoAnswer{SHA256: it.SHA256, State: photoSaved, Root: e.Root, Path: e.Path}
			break
		}
		out = append(out, answer)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// handleFinish ends an upload of the photo backup: the upload of the file
// manager (handleUploadStart/Chunk), finished with the SHA-256 the server
// worked out itself. A file with another sum than the app expected is not
// published (409 upload_hash_mismatch); the app aborts that upload. The
// finished file goes into the index of the device.
func (a appPhotosArea) handleFinish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SHA256 string `json:"sha256"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !appphotos.ValidSum(req.SHA256) {
		writeError(w, http.StatusBadRequest, "invalid_checksum", "the SHA-256 is 64 hex digits")
		return
	}
	if !a.files.authorizeUpload(w, r) {
		return
	}
	user, device := currentUser(r).Name, a.device(r)
	// Without the system data volume nothing is finished that could not be
	// kept in the index.
	if err := a.store.Ready(); err != nil {
		writePhotosError(w, err)
		return
	}
	root, err := a.files.files.UploadRoot(r.PathValue("id"), user)
	if err != nil {
		writeFileError(w, err)
		return
	}
	status, err := a.files.files.FinishUploadHashed(r.PathValue("id"), user, req.SHA256)
	if err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("photo uploaded", "name", status.Name, "bytes", status.Size, "user", user, "ip", remoteIP(r))
	recorded := false
	if st, err := statInRoot(root, status.Path); err == nil {
		err = a.store.Record(user, device, appphotos.Entry{SHA256: status.SHA256, Size: st.size, Root: root.ID, Path: status.Path, ModTime: st.modTime, At: time.Now().UTC()})
		if err != nil {
			slog.Warn("photo backup: record an upload", "err", err)
		}
		recorded = err == nil
	}
	// Not recorded (the volume went away a moment ago): the file is there,
	// and the next check finds it at its path and records it then.
	writeJSON(w, http.StatusOK, map[string]any{"id": status.ID, "size": status.Size, "offset": status.Offset, "name": status.Name,
		"path": status.Path, "sha256": status.SHA256, "root": root.ID, "recorded": recorded})
}
