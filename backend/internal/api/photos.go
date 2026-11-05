package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/thumbs"
)

// Photos in the file manager (PLAN 52, decisions of the owner of
// 5.11.2026): every account chooses its photo folders from the shares it
// sees; TitanOS lists the pictures and videos in them as a timeline (date
// taken from EXIF, else the change date), with GPS for the map. With the
// KI-Agent's picture model the photos get a short description (with text
// in the picture), started by a button and then by choice only by button,
// at night, or at night and whenever no chat runs; the search goes over
// descriptions, names, folders and dates. Albums are lists of references,
// shareable with other accounts, who see only photos of roots they may
// open themselves. Everything lies in the account's private folder of the
// agent's volume (.titanos/agent/<account>/photos*.json), never on the
// stick; nothing is copied.

const (
	photosFile       = "photos.json"
	photoAlbumsFile  = "photoalbums.json"
	photoPrefsFile   = "photoprefs.json"
	maxPhotoIndex    = 32 << 20
	maxPhotoItems    = 30000
	maxPhotoFolders  = 20
	maxPhotoFile     = 20 << 20
	photoRescanAfter = 10 * time.Minute
)

type photoFolder struct {
	Root string `json:"root"`
	Path string `json:"path"`
}

// photoPrefs: Describe is "manual", "night" or "idle" (at night and when
// no chat runs); From and To are the hours of the night window.
type photoPrefs struct {
	Folders  []photoFolder `json:"folders"`
	Describe string        `json:"describe"`
	From     int           `json:"from"`
	To       int           `json:"to"`
	Map      bool          `json:"map,omitempty"`
	// Started: the first description run was started by the button; only
	// then the schedule applies.
	Started bool `json:"started,omitempty"`
}

type photoItem struct {
	Root     string    `json:"root"`
	Path     string    `json:"path"`
	Kind     string    `json:"kind"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Taken    time.Time `json:"taken,omitzero"`
	Lat      float64   `json:"lat,omitempty"`
	Lon      float64   `json:"lon,omitempty"`
	GPS      bool      `json:"gps,omitempty"`
	Camera   string    `json:"camera,omitempty"`
	Desc     string    `json:"desc,omitempty"`
	// Failed: the picture model could not describe it (not tried again
	// until it changes).
	Failed bool `json:"failed,omitempty"`
}

func (p *photoItem) key() string { return p.Root + ":" + p.Path }

// when is the date of the timeline.
func (p *photoItem) when() time.Time {
	if !p.Taken.IsZero() {
		return p.Taken
	}
	return p.Modified
}

type photoIndex struct {
	Items   map[string]*photoItem `json:"items"`
	Scanned time.Time             `json:"scanned,omitzero"`
}

type photoAlbum struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Items   []string  `json:"items"`
	Members []string  `json:"members,omitempty"`
	Created time.Time `json:"created"`
	Owner   string    `json:"owner,omitempty"`
}

// photoUser is what titand keeps in RAM per account.
type photoUser struct {
	prefs    *photoPrefs
	index    *photoIndex
	scanning bool
	// describing: a description run; stop ends it; byPlan: started by the
	// schedule (the schedule may end it again).
	describing bool
	stop       chan struct{}
	byPlan     bool
	done       int
	total      int
	lastError  string
	model      string
}

type photoState struct {
	mu    sync.Mutex
	users map[string]*photoUser
}

func (s *Server) photoUser(name string) *photoUser {
	st := &s.photos
	if st.users == nil {
		st.users = map[string]*photoUser{}
	}
	u := st.users[name]
	if u == nil {
		u = &photoUser{}
		st.users[name] = u
	}
	return u
}

// photoPrefsOf reads the account's settings once, then keeps them in RAM.
func (s *Server) photoPrefsOf(name string) *photoPrefs {
	s.photos.mu.Lock()
	defer s.photos.mu.Unlock()
	u := s.photoUser(name)
	if u.prefs == nil {
		p := &photoPrefs{Describe: "manual", From: 1, To: 6}
		_ = s.agentStore().ReadPrivate(name, photoPrefsFile, p)
		u.prefs = p
	}
	cp := *u.prefs
	cp.Folders = slices.Clone(u.prefs.Folders)
	return &cp
}

func (s *Server) photoIndexOf(name string) *photoIndex {
	s.photos.mu.Lock()
	defer s.photos.mu.Unlock()
	u := s.photoUser(name)
	if u.index == nil {
		idx := &photoIndex{}
		_ = s.agentStore().ReadPrivateLimit(name, photosFile, idx, maxPhotoIndex)
		if idx.Items == nil {
			idx.Items = map[string]*photoItem{}
		}
		u.index = idx
	}
	return u.index
}

func (s *Server) savePhotoIndex(name string) {
	s.photos.mu.Lock()
	idx := s.photoUser(name).index
	var data []byte
	if idx != nil {
		data, _ = json.Marshal(idx)
	}
	s.photos.mu.Unlock()
	if data == nil {
		return
	}
	if err := s.agentStore().WritePrivate(name, photosFile, json.RawMessage(data)); err != nil {
		slog.Warn("photo index not saved", "account", name, "err", err)
	}
}

// photoRoots maps the root ids the account may open now to their folders.
func (s *Server) photoRoots(user auth.User) map[string]string {
	out := map[string]string{}
	for _, r := range s.fileRoots(user) {
		if r.Kind != "stick" {
			out[r.ID] = r.Dir
		}
	}
	return out
}

func hiddenName(name string) bool {
	return strings.HasPrefix(name, ".") || name == "@eaDir" || name == "#recycle"
}

// photoScan walks the chosen folders and brings the index up to date;
// descriptions of unchanged files stay.
func (s *Server) photoScan(user auth.User) {
	s.photos.mu.Lock()
	u := s.photoUser(user.Name)
	if u.scanning {
		s.photos.mu.Unlock()
		return
	}
	u.scanning = true
	s.photos.mu.Unlock()
	defer func() {
		s.photos.mu.Lock()
		s.photoUser(user.Name).scanning = false
		s.photos.mu.Unlock()
	}()
	prefs := s.photoPrefsOf(user.Name)
	old := s.photoIndexOf(user.Name)
	s.photos.mu.Lock()
	known := make(map[string]photoItem, len(old.Items))
	for k, v := range old.Items {
		known[k] = *v
	}
	s.photos.mu.Unlock()
	roots := s.photoRoots(user)
	found := map[string]*photoItem{}
	for _, f := range prefs.Folders {
		dir, ok := roots[f.Root]
		if !ok {
			continue
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			continue
		}
		start := strings.Trim(path.Clean("/"+f.Path), "/")
		if start == "" {
			start = "."
		}
		_ = fs.WalkDir(root.FS(), start, func(p string, d fs.DirEntry, err error) error {
			if err != nil || len(found) >= maxPhotoItems {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if hiddenName(d.Name()) && p != start {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			kind := thumbs.Kind(d.Name())
			if kind != "image" && kind != "video" {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			rel := strings.TrimPrefix(p, "./")
			item := &photoItem{Root: f.Root, Path: rel, Kind: kind, Size: info.Size(), Modified: info.ModTime().UTC()}
			key := item.key()
			if prev, ok := known[key]; ok && prev.Size == item.Size && prev.Modified.Equal(item.Modified) {
				cp := prev
				found[key] = &cp
				return nil
			}
			if ext := strings.ToLower(path.Ext(rel)); ext == ".jpg" || ext == ".jpeg" {
				if fh, err := root.Open(p); err == nil {
					ph := thumbs.ReadPhoto(fh)
					fh.Close()
					if !ph.Taken.IsZero() {
						item.Taken = ph.Taken
					}
					item.Lat, item.Lon, item.GPS, item.Camera = ph.Lat, ph.Lon, ph.HasGPS, ph.Camera
				}
			}
			found[key] = item
			return nil
		})
		root.Close()
	}
	s.photos.mu.Lock()
	u = s.photoUser(user.Name)
	u.index = &photoIndex{Items: found, Scanned: time.Now()}
	s.photos.mu.Unlock()
	s.savePhotoIndex(user.Name)
}

// photoList is the timeline: the account's photos of roots it may open
// now, newest first.
func (s *Server) photoList(user auth.User) []*photoItem {
	idx := s.photoIndexOf(user.Name)
	roots := s.photoRoots(user)
	s.photos.mu.Lock()
	out := make([]*photoItem, 0, len(idx.Items))
	for _, it := range idx.Items {
		if _, ok := roots[it.Root]; ok {
			cp := *it
			out = append(out, &cp)
		}
	}
	s.photos.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if a, b := out[i].when(), out[j].when(); !a.Equal(b) {
			return a.After(b)
		}
		return out[i].key() < out[j].key()
	})
	return out
}

var monthNames = []string{"januar", "februar", "märz", "april", "mai", "juni", "juli", "august", "september", "oktober", "november", "dezember"}

// photoWords normalizes text for the search: lower case, umlauts folded,
// split at everything that is no letter or digit.
func photoWords(text string) []string {
	text = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(strings.ToLower(text))
	return strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// photoSearch ranks photos by how many words of the query appear in their
// description, file name, folder and date (month name and year).
func photoSearch(list []*photoItem, query string, limit int) []*photoItem {
	words := photoWords(query)
	if len(words) == 0 {
		return nil
	}
	stop := map[string]bool{"der": true, "die": true, "das": true, "am": true, "im": true, "mit": true, "und": true, "von": true, "vom": true, "ein": true, "eine": true, "the": true, "a": true, "of": true, "in": true, "fotos": true, "foto": true, "bilder": true, "bild": true}
	type hit struct {
		p     *photoItem
		score int
	}
	var hits []hit
	for _, p := range list {
		w := p.when()
		hay := strings.Join(photoWords(p.Desc+" "+p.Path+" "+monthNames[w.Month()-1]+" "+w.Format("2006")+" "+p.Camera), " ")
		score, need := 0, 0
		for _, q := range words {
			if stop[q] {
				continue
			}
			need++
			if strings.Contains(hay, q) || (len(q) > 4 && strings.Contains(hay, q[:len(q)-1])) {
				score++
			}
		}
		if need > 0 && score > 0 && score*2 >= need {
			hits = append(hits, hit{p, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]*photoItem, 0, min(len(hits), limit))
	for _, h := range hits {
		if len(out) >= limit {
			break
		}
		out = append(out, h.p)
	}
	return out
}

// ---- Descriptions ---------------------------------------------------------

const photoPrompt = "Beschreibe dieses Foto für eine Suche, auf Deutsch: zuerst ein bis zwei Sätze, was zu sehen ist (Ort, Dinge, Tiere, Anzahl der Personen ohne Namen, Stimmung, Jahreszeit), dann eine Zeile \"Stichwörter:\" mit 5 bis 10 Stichwörtern. Steht Text im Bild (Schild, Beleg, Bildschirm), gib die wichtigsten Wörter wörtlich in einer Zeile \"Text:\" wieder. Nichts erfinden."

// photoAI tells whether the account may have its photos described and
// which model looks at them.
func (s *Server) photoAI(ctx context.Context, user auth.User) (*agent.Ollama, string, error) {
	settings := s.agentSettings()
	if !settings.Enabled || !s.agentAllowed(user) {
		return nil, "", errors.New("der KI-Agent ist aus oder für dieses Konto nicht erlaubt")
	}
	client, err := s.agentOllama()
	if err != nil {
		return nil, "", err
	}
	box := &agentToolbox{s: s, user: user, settings: settings}
	model := box.imageModel(ctx, client, settings.Model)
	if model == "" {
		return nil, "", errors.New("kein Modell, das Bilder sieht (Einstellungen › KI-Agent › Bilder)")
	}
	return client, model, nil
}

// photoDescribe starts a description run for the account (byPlan: from the
// schedule). It describes pictures without a description, newest first,
// one after the other, and waits while a chat of the agent runs.
func (s *Server) photoDescribe(user auth.User, byPlan bool) error {
	ctx, cancel := context.WithTimeout(s.agentBase(), 10*time.Second)
	client, model, err := s.photoAI(ctx, user)
	cancel()
	if err != nil {
		return err
	}
	s.photos.mu.Lock()
	u := s.photoUser(user.Name)
	if u.describing {
		s.photos.mu.Unlock()
		return nil
	}
	u.describing, u.byPlan, u.stop, u.lastError, u.model = true, byPlan, make(chan struct{}), "", model
	stop := u.stop
	s.photos.mu.Unlock()
	go s.photoDescribeRun(user, client, model, stop)
	return nil
}

func (s *Server) photoStopDescribe(name string, onlyPlanned bool) {
	s.photos.mu.Lock()
	defer s.photos.mu.Unlock()
	u := s.photoUser(name)
	if u.describing && u.stop != nil && (!onlyPlanned || u.byPlan) {
		close(u.stop)
		u.stop = nil
	}
}

// photoBusy: a chat of the agent runs (the photos wait).
func (s *Server) photoBusy() bool {
	s.agentRT.mu.Lock()
	defer s.agentRT.mu.Unlock()
	return len(s.agentRT.runs) > 0
}

func (s *Server) photoDescribeRun(user auth.User, client *agent.Ollama, model string, stop chan struct{}) {
	defer func() {
		s.photos.mu.Lock()
		u := s.photoUser(user.Name)
		u.describing, u.stop = false, nil
		s.photos.mu.Unlock()
		s.savePhotoIndex(user.Name)
	}()
	list := s.photoList(user)
	var todo []*photoItem
	for _, p := range list {
		if p.Kind == "image" && p.Desc == "" && !p.Failed && p.Size <= maxPhotoFile {
			todo = append(todo, p)
		}
	}
	s.photos.mu.Lock()
	u := s.photoUser(user.Name)
	u.done, u.total = 0, len(todo)
	s.photos.mu.Unlock()
	roots := s.photoRoots(user)
	settings := s.agentSettings()
	for i, p := range todo {
		for s.photoBusy() {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Second):
			}
		}
		select {
		case <-stop:
			return
		default:
		}
		desc, err := s.photoDescribeOne(roots, p, client, model, settings.KeepAlive)
		s.photos.mu.Lock()
		u := s.photoUser(user.Name)
		if it := u.index.Items[p.key()]; it != nil {
			if err != nil {
				it.Failed = true
				u.lastError = err.Error()
			} else {
				it.Desc = desc
			}
		}
		u.done = i + 1
		s.photos.mu.Unlock()
		if errors.Is(err, agent.ErrUnreachable) {
			return
		}
		if (i+1)%10 == 0 {
			s.savePhotoIndex(user.Name)
		}
	}
}

func (s *Server) photoDescribeOne(roots map[string]string, p *photoItem, client *agent.Ollama, model, keepAlive string) (string, error) {
	dir, ok := roots[p.Root]
	if !ok {
		return "", errors.New("no access")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	f, err := root.Open(p.Path)
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPhotoFile+1))
	f.Close()
	if err != nil || len(data) > maxPhotoFile {
		return "", errors.New("unreadable or too large")
	}
	ctx, cancel := context.WithTimeout(s.agentBase(), 3*time.Minute)
	defer cancel()
	text, err := client.Look(ctx, model, photoPrompt, data, keepAlive)
	if err != nil {
		return "", err
	}
	if r := []rune(text); len(r) > 1200 {
		text = string(r[:1200])
	}
	return strings.TrimSpace(text), nil
}

// photoTick runs the schedules of the description runs (every minute, with
// the agent's own tick).
func (s *Server) photoTick(now time.Time) {
	if s.opts.Users == nil {
		return
	}
	busy := s.photoBusy()
	for _, name := range s.agentAccounts() {
		prefs := s.photoPrefsOf(name)
		if !prefs.Started || prefs.Describe == "manual" || prefs.Describe == "" {
			continue
		}
		h := now.In(s.agentLocation()).Hour()
		night := prefs.From == prefs.To || (prefs.From < prefs.To && h >= prefs.From && h < prefs.To) || (prefs.From > prefs.To && (h >= prefs.From || h < prefs.To))
		want := night || (prefs.Describe == "idle" && !busy)
		s.photos.mu.Lock()
		running := s.photoUser(name).describing
		s.photos.mu.Unlock()
		switch {
		case want && !running:
			if user, ok := s.opts.Users.Get(name); ok && s.photoPending(user) {
				_ = s.photoDescribe(user, true)
			}
		case !want && running:
			s.photoStopDescribe(name, true)
		}
	}
}

// photoPending: there are photos without a description.
func (s *Server) photoPending(user auth.User) bool {
	for _, p := range s.photoList(user) {
		if p.Kind == "image" && p.Desc == "" && !p.Failed {
			return true
		}
	}
	return false
}

// ---- HTTP -------------------------------------------------------------------

func (s *Server) handlePhotos(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	prefs := s.photoPrefsOf(user.Name)
	idx := s.photoIndexOf(user.Name)
	s.photos.mu.Lock()
	stale := time.Since(idx.Scanned) > photoRescanAfter
	s.photos.mu.Unlock()
	if stale && len(prefs.Folders) > 0 {
		go s.photoScan(user)
	}
	list := s.photoList(user)
	described := 0
	for _, p := range list {
		if p.Desc != "" {
			described++
		}
	}
	s.photos.mu.Lock()
	u := s.photoUser(user.Name)
	state := map[string]any{"running": u.describing, "done": u.done, "total": u.total, "error": u.lastError, "model": u.model}
	scanning, scanned := u.scanning || stale && len(prefs.Folders) > 0, idx.Scanned
	s.photos.mu.Unlock()
	settings := s.agentSettings()
	writeJSON(w, http.StatusOK, map[string]any{
		"prefs": prefs, "items": list, "described": described,
		"scanning": scanning, "scanned": scanned, "describe": state,
		"ai": settings.Enabled && s.agentAllowed(user),
	})
}

func (s *Server) handlePhotoPrefs(w http.ResponseWriter, r *http.Request) {
	var req photoPrefs
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if len(req.Folders) > maxPhotoFolders {
		writeError(w, http.StatusBadRequest, "invalid_photos", "at most 20 folders")
		return
	}
	roots := s.photoRoots(user)
	var folders []photoFolder
	for _, f := range req.Folders {
		f.Path = strings.Trim(path.Clean("/"+f.Path), "/")
		if _, ok := roots[f.Root]; !ok {
			writeError(w, http.StatusBadRequest, "invalid_photos", "no such root: "+f.Root)
			return
		}
		if !slices.Contains(folders, f) {
			folders = append(folders, f)
		}
	}
	switch req.Describe {
	case "manual", "night", "idle":
	case "":
		req.Describe = "manual"
	default:
		writeError(w, http.StatusBadRequest, "invalid_photos", "describe: manual, night or idle")
		return
	}
	if req.From < 0 || req.From > 23 || req.To < 0 || req.To > 23 {
		writeError(w, http.StatusBadRequest, "invalid_photos", "hours 0–23")
		return
	}
	req.Folders = folders
	s.photos.mu.Lock()
	u := s.photoUser(user.Name)
	if u.prefs != nil {
		req.Started = req.Started || u.prefs.Started
	}
	changed := u.prefs == nil || !slices.Equal(u.prefs.Folders, folders)
	cp := req
	u.prefs = &cp
	if changed && u.index != nil {
		u.index.Scanned = time.Time{}
	}
	s.photos.mu.Unlock()
	if err := s.agentStore().WritePrivate(user.Name, photoPrefsFile, req); err != nil {
		agentStoreError(w, err)
		return
	}
	if changed {
		go s.photoScan(user)
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) handlePhotoScan(w http.ResponseWriter, r *http.Request) {
	go s.photoScan(currentUser(r))
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handlePhotoDescribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Run bool `json:"run"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if !req.Run {
		s.photoStopDescribe(user.Name, false)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.photoDescribe(user, false); err != nil {
		writeError(w, http.StatusConflict, "photos_ai", err.Error())
		return
	}
	// The first run by the button lets the schedule take over.
	s.photos.mu.Lock()
	u := s.photoUser(user.Name)
	var save *photoPrefs
	if u.prefs != nil && !u.prefs.Started {
		u.prefs.Started = true
		cp := *u.prefs
		save = &cp
	}
	s.photos.mu.Unlock()
	if save != nil {
		_ = s.agentStore().WritePrivate(user.Name, photoPrefsFile, save)
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handlePhotoSearch(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	hits := photoSearch(s.photoList(user), r.URL.Query().Get("q"), 300)
	if hits == nil {
		hits = []*photoItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": hits})
}

// ---- Albums ---------------------------------------------------------------

func (s *Server) photoAlbums(name string) []photoAlbum {
	var list []photoAlbum
	_ = s.agentStore().ReadPrivateLimit(name, photoAlbumsFile, &list, maxPhotoIndex)
	return list
}

// photoAlbumsFor are the account's own albums and those shared with it,
// each with only the photos of roots the account may open.
func (s *Server) photoAlbumsFor(user auth.User) []photoAlbum {
	roots := s.photoRoots(user)
	visible := func(a photoAlbum) photoAlbum {
		items := []string{}
		for _, k := range a.Items {
			if root, _, ok := strings.Cut(k, ":"); ok {
				if _, ok := roots[root]; ok {
					items = append(items, k)
				}
			}
		}
		a.Items = items
		return a
	}
	var out []photoAlbum
	for _, a := range s.photoAlbums(user.Name) {
		out = append(out, visible(a))
	}
	for _, u := range s.opts.Users.List() {
		if u.Name == user.Name {
			continue
		}
		for _, a := range s.photoAlbums(u.Name) {
			if slices.Contains(a.Members, user.Name) {
				a.Owner = u.Name
				out = append(out, visible(a))
			}
		}
	}
	if out == nil {
		out = []photoAlbum{}
	}
	return out
}

func (s *Server) handlePhotoAlbums(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"albums": s.photoAlbumsFor(currentUser(r))})
}

// photoAlbumSave creates an album or changes one of the account's own:
// name, members, and photos to add or remove (keys root:path of photos
// the account may open).
func (s *Server) photoAlbumSave(user auth.User, id, name string, members []string, add, remove []string) (photoAlbum, error) {
	list := s.photoAlbums(user.Name)
	roots := s.photoRoots(user)
	i := slices.IndexFunc(list, func(a photoAlbum) bool { return a.ID == id })
	if id == "" || id == "new" {
		if len(list) >= 200 {
			return photoAlbum{}, errors.New("at most 200 albums")
		}
		list = append(list, photoAlbum{ID: agent.NewID(), Created: time.Now(), Items: []string{}})
		i = len(list) - 1
	} else if i < 0 {
		return photoAlbum{}, agent.ErrNotFound
	}
	a := &list[i]
	if name = strings.TrimSpace(name); name != "" {
		if len(name) > 80 {
			return photoAlbum{}, errors.New("name at most 80 characters")
		}
		a.Name = name
	}
	if a.Name == "" {
		return photoAlbum{}, errors.New("an album needs a name")
	}
	if members != nil {
		var ms []string
		for _, m := range members {
			if _, ok := s.opts.Users.Get(m); ok && m != user.Name && !slices.Contains(ms, m) {
				ms = append(ms, m)
			}
		}
		a.Members = ms
	}
	for _, k := range add {
		root, p, ok := strings.Cut(k, ":")
		if _, allowed := roots[root]; !ok || !allowed || p == "" || strings.Contains(p, "..") {
			continue
		}
		if !slices.Contains(a.Items, k) && len(a.Items) < 5000 {
			a.Items = append(a.Items, k)
		}
	}
	a.Items = slices.DeleteFunc(a.Items, func(k string) bool { return slices.Contains(remove, k) })
	if err := s.agentStore().WritePrivate(user.Name, photoAlbumsFile, list); err != nil {
		return photoAlbum{}, err
	}
	return *a, nil
}

func (s *Server) handlePhotoAlbumSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
		Add     []string `json:"add"`
		Remove  []string `json:"remove"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	a, err := s.photoAlbumSave(currentUser(r), r.PathValue("id"), req.Name, req.Members, req.Add, req.Remove)
	if errors.Is(err, agent.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such album")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_album", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// handlePhotoAccounts lists the names of the other accounts, to share an
// album with (names only).
func (s *Server) handlePhotoAccounts(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r).Name
	names := []string{}
	for _, u := range s.opts.Users.List() {
		if u.Name != me && !u.Disabled {
			names = append(names, u.Name)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": names})
}

func (s *Server) handlePhotoAlbumDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	list := s.photoAlbums(user.Name)
	n := len(list)
	list = slices.DeleteFunc(list, func(a photoAlbum) bool { return a.ID == r.PathValue("id") })
	if len(list) == n {
		writeError(w, http.StatusNotFound, "not_found", "no such album")
		return
	}
	if err := s.agentStore().WritePrivate(user.Name, photoAlbumsFile, list); err != nil {
		agentStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
