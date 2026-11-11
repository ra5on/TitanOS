package api

import (
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/configbackup"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/sysdata"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// sysDataArea is the page "Systemdaten" (PLAN 79-8): where TitanOS keeps
// its own data. One volume for everything and one per function; the
// choices of the activity log, the copies of /config and the monitoring
// history stay in their own settings and are only gathered here. An area
// with narrow dependencies (PLAN 8a, see fansArea).
type sysDataArea struct {
	data       *sysdata.Manager
	home       *volumedata.Home
	storage    *storage.Manager
	activity   *activity.Manager
	backup     *configbackup.Manager
	monitoring *monitoring.Manager
}

func (a sysDataArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/system/data", admin(a.handleSystemData))
	mux.HandleFunc("PUT /api/v1/system/data", admin(a.handleSystemDataSet))
}

// sysDataVolume is a volume as the page offers it.
type sysDataVolume struct {
	Name    string `json:"name"`
	Mounted bool   `json:"mounted"`
	// Sleeps: at least one disk of the volume may be put to sleep, so
	// system data would wake it again and again.
	Sleeps bool `json:"sleeps"`
}

// sysDataFunction is one function with a place of its own.
type sysDataFunction struct {
	ID string `json:"id"`
	// Choice is the volume chosen for this function, "" while it follows
	// the common one.
	Choice string `json:"choice"`
	// Volume is where its data go now, "" while there is no volume.
	Volume string `json:"volume"`
	// Source says where Volume comes from: "own" (Choice), "common" (the
	// common choice) or "automatic" (TitanOS chose).
	Source string `json:"source"`
	// Enabled is false for a function that is switched off.
	Enabled bool `json:"enabled"`
	// Moving: the folder is being moved to Volume right now.
	Moving bool `json:"moving"`
	// SizeBytes is the size on Volume as last measured; null while it was
	// never measured (the disks of a sleeping volume are not woken for it).
	SizeBytes *int64     `json:"sizeBytes"`
	SizeAt    *time.Time `json:"sizeAt"`
}

type sysDataState struct {
	// Volume is the common choice, "" for the automatic one.
	Volume string `json:"volume"`
	// Resolved is the volume in use for everything without its own choice.
	Resolved string `json:"resolved"`
	// SleepKnown is false while it cannot be told which volumes may sleep.
	SleepKnown bool              `json:"sleepKnown"`
	Volumes    []sysDataVolume   `json:"volumes"`
	Functions  []sysDataFunction `json:"functions"`
}

func (a sysDataArea) available() bool {
	return a.data != nil && a.home != nil && a.storage != nil
}

// choice is the own volume of a function and whether it is switched on.
func (a sysDataArea) choice(function string) (volume string, enabled bool) {
	switch function {
	case sysdata.Config:
		if a.backup == nil {
			return "", false
		}
		s := a.backup.Settings()
		return s.Volume, s.Enabled
	case sysdata.Activity:
		if a.activity == nil {
			return "", false
		}
		s := a.activity.Settings()
		return s.Volume, s.Enabled
	case sysdata.Monitoring:
		if a.monitoring == nil {
			return "", false
		}
		s := a.monitoring.Status().Settings
		if s.FollowSystemData {
			return "", s.Enabled
		}
		return s.Volume, s.Enabled
	}
	return a.data.Override(function), true
}

func (a sysDataArea) state(r *http.Request) sysDataState {
	mounted := a.storage.Mounted()
	sleeping, known := a.storage.SleepingVolumes(r.Context())
	state := sysDataState{Volume: a.data.Volume(), Resolved: a.home.Default(), SleepKnown: known, Volumes: []sysDataVolume{}, Functions: []sysDataFunction{}}
	names := a.storage.Names()
	slices.Sort(names)
	for _, name := range names {
		state.Volumes = append(state.Volumes, sysDataVolume{Name: name, Mounted: slices.Contains(mounted, name), Sleeps: sleeping[name]})
	}
	for _, id := range sysdata.Functions {
		choice, enabled := a.choice(id)
		f := sysDataFunction{ID: id, Choice: choice, Enabled: enabled, Volume: choice, Source: "own"}
		if choice == "" {
			f.Volume, f.Source = state.Resolved, "automatic"
			if state.Volume != "" && state.Volume == state.Resolved {
				f.Source = "common"
			}
		}
		if !slices.Contains(mounted, f.Volume) {
			f.Volume = ""
		}
		if id != sysdata.Config {
			f.Moving = a.home.Moving(sysdata.Folder(id))
		}
		if bytes, at := a.data.Size(f.Volume, id); !at.IsZero() {
			f.SizeBytes, f.SizeAt = &bytes, &at
		}
		state.Functions = append(state.Functions, f)
	}
	return state
}

func (a sysDataArea) handleSystemData(w http.ResponseWriter, r *http.Request) {
	if !a.available() {
		featureUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, a.state(r))
}

// handleSystemDataSet stores the common volume and the volumes of single
// functions. Only what the request names is changed; "" means "follow the
// common volume" (for volume: let TitanOS choose). The data of a function
// whose volume changes are moved in the background.
func (a sysDataArea) handleSystemDataSet(w http.ResponseWriter, r *http.Request) {
	if !a.available() {
		featureUnavailable(w)
		return
	}
	var req struct {
		Volume    *string           `json:"volume"`
		Functions map[string]string `json:"functions"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	mounted := a.storage.Mounted()
	before := a.state(r)
	was := map[string]sysDataFunction{}
	for _, f := range before.Functions {
		was[f.ID] = f
	}
	// A newly chosen volume must be mounted: its data are moved there now.
	usable := func(volume, current string) bool {
		return volume == "" || volume == current || slices.Contains(mounted, volume)
	}
	if req.Volume != nil && !usable(*req.Volume, before.Volume) {
		writeError(w, http.StatusBadRequest, "invalid_volume", "the volume is not mounted")
		return
	}
	for id, volume := range req.Functions {
		f, ok := was[id]
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "unknown function "+id)
			return
		}
		if !usable(volume, f.Choice) {
			writeError(w, http.StatusBadRequest, "invalid_volume", "the volume is not mounted")
			return
		}
		switch {
		case id == sysdata.Config && a.backup == nil, id == sysdata.Activity && a.activity == nil, id == sysdata.Monitoring && a.monitoring == nil:
			featureUnavailable(w)
			return
		}
	}
	common := before.Volume
	if req.Volume != nil {
		common = *req.Volume
	}
	// The volume in use for everything without its own choice afterwards.
	resolved := common
	if resolved == "" {
		resolved = a.storage.SystemDataVolume(r.Context())
	}
	// The settings kept elsewhere first: a refusal there changes nothing.
	if volume, ok := req.Functions[sysdata.Config]; ok && volume != was[sysdata.Config].Choice {
		s := a.backup.Settings()
		s.Volume = volume
		if err := a.backup.SetSettings(s, a.storage.Names()); err != nil {
			writeBackupError(w, err)
			return
		}
	}
	if volume, ok := req.Functions[sysdata.Activity]; ok && volume != was[sysdata.Activity].Choice {
		s := a.activity.Settings()
		s.Volume = volume
		if err := a.activity.Set(s); err != nil {
			featureError(w, err)
			return
		}
	}
	if a.monitoring != nil {
		s := a.monitoring.Status().Settings
		next := s
		if volume, ok := req.Functions[sysdata.Monitoring]; ok {
			next.FollowSystemData, next.Volume = volume == "", volume
		}
		if next.FollowSystemData && resolved != "" {
			// Versions before 0.77 read Volume: keep it a real one.
			next.Volume = resolved
		}
		if next.FollowSystemData && next.Volume == "" {
			next.Volume = s.Volume
		}
		if next != s {
			if err := a.monitoring.Set(next); err != nil {
				featureError(w, err)
				return
			}
		}
	}
	err := a.data.Update(func(s *sysdata.Settings) {
		s.Volume = common
		for id, volume := range req.Functions {
			if sysdata.Stored(id) {
				s.Overrides[id] = volume
			}
		}
		// What lies on the volume used so far is moved; the note stays in
		// /config until the move is through, also over a restart.
		for _, id := range sysdata.Functions {
			// Not a.choice for the ones kept here: Update holds their lock.
			target := s.Overrides[id]
			if !sysdata.Stored(id) {
				target, _ = a.choice(id)
			}
			if target == "" {
				target = resolved
			}
			if from := was[id].Volume; from != "" && target != "" && from != target {
				sysdata.AddPending(s, id, from)
			}
		}
	})
	if err != nil {
		featureError(w, err)
		return
	}
	slog.Info("system data volumes changed", "volume", common, "functions", req.Functions, "by", currentUser(r).Name, "ip", remoteIP(r))
	// Start the moves now instead of at the next write of each function.
	go func() {
		for _, id := range sysdata.Functions {
			if id != sysdata.Config {
				a.home.Volume(sysdata.Folder(id))
			}
		}
	}()
	writeJSON(w, http.StatusOK, a.state(r))
}
