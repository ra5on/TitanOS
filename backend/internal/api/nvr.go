package api

// The NVR (PLAN 79-7, docs/NVR.md): Frigate in a container TitanOS manages,
// its settings and cameras, and who may see which camera. None of these
// routes is in publicRoutes: they are for the web UI only (no tokens, no
// MCP, no agent). Live pictures are in nvr_live.go.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// nvrArea is the NVR; an area with narrow dependencies (PLAN 8a, see
// fansArea). nvr is nil where there is none; users are the accounts;
// loginHolds tells whether the login of a request still stands (live
// views end with it, like consoles).
type nvrArea struct {
	nvr        *nvr.Manager
	users      *auth.Users
	loginHolds func(r *http.Request, user auth.User) (auth.User, bool)
}

// nvrArea is the area of the server.
func (s *Server) nvrArea() nvrArea {
	return nvrArea{nvr: s.opts.NVR, users: s.opts.Users, loginHolds: s.loginHolds}
}

// registerNVR registers the routes of the NVR.
func (s *Server) registerNVR(mux *routeMux) {
	s.nvrArea().register(mux, s.guards())
}

func (a nvrArea) register(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/nvr/status", g.user(a.handleStatus))
	mux.HandleFunc("GET /api/v1/nvr/settings", g.admin(a.handleSettings))
	mux.HandleFunc("PUT /api/v1/nvr/settings", g.admin(a.handleSettingsSet))
	mux.HandleFunc("POST /api/v1/nvr/enable", g.admin(a.handleEnable))
	mux.HandleFunc("POST /api/v1/nvr/disable", g.admin(a.handleDisable))
	mux.HandleFunc("GET /api/v1/nvr/diagnosis", g.admin(a.handleDiagnosis))
	mux.HandleFunc("POST /api/v1/nvr/detector/retry", g.admin(a.handleRetry))
	mux.HandleFunc("GET /api/v1/nvr/cameras", g.user(a.handleCameras))
	mux.HandleFunc("POST /api/v1/nvr/cameras", g.admin(a.handleCameraAdd))
	mux.HandleFunc("POST /api/v1/nvr/cameras/discover", g.admin(a.handleDiscover))
	mux.HandleFunc("POST /api/v1/nvr/cameras/probe", g.admin(a.handleProbe))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}", g.user(a.handleCamera))
	mux.HandleFunc("PUT /api/v1/nvr/cameras/{id}", g.admin(a.handleCameraSet))
	mux.HandleFunc("DELETE /api/v1/nvr/cameras/{id}", g.admin(a.handleCameraRemove))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/snapshot.jpg", g.user(a.handleSnapshot))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/live", g.user(a.handleLive))
	// Next to the apps and shares of an account (Konten und Zugang › Nutzer).
	mux.HandleFunc("GET /api/v1/users/{name}/cameras", g.admin(a.handleUserCameras))
	mux.HandleFunc("PUT /api/v1/users/{name}/cameras", g.admin(a.handleUserCamerasSet))
}

// available answers 404 where there is no NVR.
func (a nvrArea) available(w http.ResponseWriter) bool {
	if a.nvr == nil {
		writeError(w, http.StatusNotFound, "nvr_unavailable", "this NAS has no NVR")
		return false
	}
	return true
}

// allows tells whether the account of the request has a right on a camera
// now (owners and administrators: always).
func (a nvrArea) allows(user auth.User, camera, right string) bool {
	if a.nvr == nil || a.nvr.Access == nil {
		return user.Role.CanAdminister()
	}
	return a.nvr.Access.Allows(user.Name, camera, right)
}

// writeNVRError maps an error of the NVR to its code.
func writeNVRError(w http.ResponseWriter, err error) {
	for _, known := range []struct {
		err     error
		status  int
		code    string
		message string
	}{
		{nvr.ErrDockerOff, http.StatusConflict, "nvr_docker_off", "Docker is switched off; the NVR needs it"},
		{nvr.ErrNoVolume, http.StatusConflict, "nvr_no_volume", "choose a mounted volume for the NVR"},
		{nvr.ErrVolumeSleeps, http.StatusConflict, "nvr_volume_sleeps", "the disks of this volume may sleep; recordings keep them awake"},
		{nvr.ErrTargetNotAllowed, http.StatusConflict, "nvr_target_public", "the address is not in the home network"},
		{nvr.ErrNotFound, http.StatusNotFound, "nvr_camera_not_found", "no such camera"},
		{nvr.ErrExists, http.StatusConflict, "nvr_camera_exists", "a camera with this ID exists"},
		{nvr.ErrLimit, http.StatusConflict, "nvr_camera_limit", "no more cameras can be added"},
		{nvr.ErrNotRunning, http.StatusConflict, "nvr_not_running", "the NVR does not run"},
		{nvr.ErrDisabled, http.StatusConflict, "nvr_disabled", "the NVR is switched off"},
		{nvr.ErrNotLoaded, http.StatusServiceUnavailable, "config_unavailable", "the configuration could not be read from the stick"},
	} {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, known.message)
			return
		}
	}
	if errors.Is(err, nvr.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "nvr_invalid", err.Error())
		return
	}
	slog.Error("NVR request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "the NVR could not do this")
}

// nvrStatusView is GET /api/v1/nvr/status for an account that does not
// administer: whether the NVR runs and how many of its cameras there are.
type nvrStatusView struct {
	Enabled bool             `json:"enabled"`
	State   string           `json:"state"`
	Cameras nvr.CameraCounts `json:"cameras"`
	// PrivacyNoticeDismissed: the notice on filming people is hidden.
	PrivacyNoticeDismissed bool `json:"privacyNoticeDismissed"`
}

// handleStatus reports the NVR. Owners and administrators get everything
// (nvr.Status); other accounts only the state and the counts of the
// cameras they may see: no hardware, no errors, no storage.
func (a nvrArea) handleStatus(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if a.nvr == nil {
		if user.Role.CanAdminister() {
			writeJSON(w, http.StatusOK, nvr.Status{State: nvr.PhaseOff, Image: nvr.ImageStatus{State: "missing"}, Hardware: nvr.HardwareInfo{Devices: []nvr.Device{}}})
		} else {
			writeJSON(w, http.StatusOK, nvrStatusView{State: nvr.PhaseOff})
		}
		return
	}
	status := a.nvr.Status(r.Context())
	if user.Role.CanAdminister() {
		writeJSON(w, http.StatusOK, status)
		return
	}
	view := nvrStatusView{Enabled: status.Enabled, State: status.State, PrivacyNoticeDismissed: status.PrivacyNoticeDismissed}
	// The reasons the NVR does not run are the administrator's business.
	switch status.State {
	case nvr.PhaseRunning, nvr.PhaseOff, nvr.PhaseStarting, nvr.PhasePulling:
	default:
		view.State = "unavailable"
	}
	states := a.nvr.CameraStates()
	for _, c := range a.nvr.Cameras() {
		if !a.nvr.Access.Sees(user.Name, c.ID) {
			continue
		}
		view.Cameras.Total++
		if c.Enabled {
			view.Cameras.Enabled++
			if states[c.ID].Online {
				view.Cameras.Online++
			}
		}
	}
	writeJSON(w, http.StatusOK, view)
}

// nvrSettingsView are the settings as administrators see them. The
// password of the restream is shown: they type it into Home Assistant.
type nvrSettingsView struct {
	Enabled                bool                `json:"enabled"`
	Volume                 string              `json:"volume"`
	Detector               nvr.DetectorSetting `json:"detector"`
	Storage                nvr.StorageSetting  `json:"storage"`
	PrivacyNoticeDismissed bool                `json:"privacyNoticeDismissed"`
	RTSPOut                bool                `json:"rtspOut"`
	// RTSPUser and RTSPPassword are the login of the restream on port
	// 8554 ("" while it was never switched on).
	RTSPUser     string `json:"rtspUser"`
	RTSPPassword string `json:"rtspPassword"`
}

func settingsView(s nvr.Settings) nvrSettingsView {
	view := nvrSettingsView{Enabled: s.Enabled, Volume: s.Volume, Detector: s.Detector, Storage: s.Storage,
		PrivacyNoticeDismissed: s.PrivacyNoticeDismissed, RTSPOut: s.RTSPOut}
	if s.RTSPOut {
		view.RTSPUser, view.RTSPPassword = nvr.RTSPUser, s.RTSPPassword
	}
	return view
}

func (a nvrArea) handleSettings(w http.ResponseWriter, r *http.Request) {
	if a.nvr == nil {
		writeJSON(w, http.StatusOK, settingsView(nvr.Defaults()))
		return
	}
	writeJSON(w, http.StatusOK, settingsView(a.nvr.Settings()))
}

// nvrConfirmations are the administrator's answers to the two questions
// a change of the settings can raise.
type nvrConfirmations struct {
	// EnableDocker answers nvr_docker_off: switch Docker on.
	EnableDocker bool `json:"enableDocker"`
	// ConfirmSleepingVolume answers nvr_volume_sleeps.
	ConfirmSleepingVolume bool `json:"confirmSleepingVolume"`
}

func (c nvrConfirmations) options() nvr.SetOptions {
	return nvr.SetOptions{EnableDocker: c.EnableDocker, ConfirmSleepingVolume: c.ConfirmSleepingVolume}
}

// handleSettingsSet stores {enabled, volume, detector: {kind}, storage:
// {limitGB}, privacyNoticeDismissed, rtspOut}; fields left out stay.
func (a nvrArea) handleSettingsSet(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	current := a.nvr.Settings()
	req := struct {
		nvrSettingsView
		nvrConfirmations
	}{nvrSettingsView: settingsView(current)}
	if !readJSON(w, r, &req) {
		return
	}
	next := current
	next.Enabled, next.Volume, next.Detector, next.Storage = req.Enabled, req.Volume, req.Detector, req.Storage
	next.PrivacyNoticeDismissed, next.RTSPOut = req.PrivacyNoticeDismissed, req.RTSPOut
	if err := a.nvr.SetSettings(r.Context(), next, req.options()); err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, "nvr")
	slog.Info("NVR settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, settingsView(a.nvr.Settings()))
}

// handleEnable switches the NVR on: {volume, enableDocker,
// confirmSleepingVolume}. With Docker switched off it answers
// nvr_docker_off; the page asks "switch Docker on?" and sends the request
// again with enableDocker. The image is then fetched and Frigate started
// in the background: the status shows how far it is.
func (a nvrArea) handleEnable(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var req struct {
		Volume string `json:"volume"`
		nvrConfirmations
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.nvr.Enable(r.Context(), req.Volume, req.options()); err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, "nvr")
	slog.Info("NVR switched on", "by", currentUser(r).Name, "ip", remoteIP(r), "dockerSwitchedOn", req.EnableDocker)
	writeJSON(w, http.StatusOK, a.nvr.Status(r.Context()))
}

// handleDisable switches the NVR off: the container goes, settings,
// cameras and recordings stay.
func (a nvrArea) handleDisable(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	if err := a.nvr.Disable(r.Context()); err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, "nvr")
	slog.Info("NVR switched off", "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, a.nvr.Status(r.Context()))
}

func (a nvrArea) handleDiagnosis(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	writeJSON(w, http.StatusOK, a.nvr.Diagnosis(r.Context()))
}

// handleRetry goes back to the chosen detection device.
func (a nvrArea) handleRetry(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	if err := a.nvr.RetryDetector(); err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, "nvr")
	writeJSON(w, http.StatusOK, a.nvr.Status(r.Context()))
}

// nvrCameraBrief is a camera as an account sees it that does not
// administer: what it needs to show the picture, nothing of the source.
type nvrCameraBrief struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Enabled bool            `json:"enabled"`
	Order   int             `json:"order"`
	Audio   nvr.Audio       `json:"audio"`
	PTZ     bool            `json:"ptz"`
	State   nvr.CameraState `json:"state"`
	// Rights are the rights of the asking account on this camera.
	Rights nvrCameraRights `json:"rights"`
}

type nvrCameraRights struct {
	Live       bool `json:"live"`
	Recordings bool `json:"recordings"`
}

func (a nvrArea) brief(user auth.User, c nvr.Camera, state nvr.CameraState) nvrCameraBrief {
	return nvrCameraBrief{ID: c.ID, Name: c.Name, Enabled: c.Enabled, Order: c.Order, Audio: c.Audio, PTZ: c.PTZ, State: state,
		Rights: nvrCameraRights{Live: a.allows(user, c.ID, nvr.RightLive), Recordings: a.allows(user, c.ID, nvr.RightRecordings)}}
}

// handleCameras lists the cameras: {cameras: […]}. Owners and
// administrators get every camera in full (nvr.CameraView, never a
// password); other accounts the cameras they may see, without the source.
func (a nvrArea) handleCameras(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if a.nvr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"cameras": []any{}, "objects": nvr.AllowedObjects()})
		return
	}
	states := a.nvr.CameraStates()
	if user.Role.CanAdminister() {
		out := []nvr.CameraView{}
		for _, c := range a.nvr.Cameras() {
			out = append(out, c.View(states[c.ID]))
		}
		writeJSON(w, http.StatusOK, map[string]any{"cameras": out, "objects": nvr.AllowedObjects()})
		return
	}
	out := []nvrCameraBrief{}
	for _, c := range a.nvr.Cameras() {
		if a.nvr.Access.Sees(user.Name, c.ID) {
			out = append(out, a.brief(user, c, states[c.ID]))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cameras": out})
}

// handleCamera returns one camera, as handleCameras lists it. A camera
// the account may not see is answered like one that does not exist.
func (a nvrArea) handleCamera(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	user := currentUser(r)
	c, ok := a.nvr.Camera(r.PathValue("id"))
	if !ok || (!user.Role.CanAdminister() && !a.nvr.Access.Sees(user.Name, c.ID)) {
		writeNVRError(w, nvr.ErrNotFound)
		return
	}
	state := a.nvr.CameraStates()[c.ID]
	if user.Role.CanAdminister() {
		writeJSON(w, http.StatusOK, c.View(state))
		return
	}
	writeJSON(w, http.StatusOK, a.brief(user, c, state))
}

// nvrCameraRequest is a camera as the page sends it: nvr.Camera with the
// password in source.password (empty: keep the stored one) and
// clearPassword to drop it.
type nvrCameraRequest struct {
	nvr.Camera
	ClearPassword bool `json:"clearPassword"`
}

// fromRequest tidies what people paste: a stream address with the login
// in it gives its login to the fields for it.
func (req *nvrCameraRequest) tidy() {
	s := &req.Source
	for _, address := range []*string{&s.MainURL, &s.SubURL} {
		clean, user, password := nvr.SplitCredentials(*address)
		*address = clean
		if user != "" && s.User == "" {
			s.User = user
		}
		if password != "" && s.Password == "" && s.User == user {
			s.Password = password
		}
	}
}

// handleCameraAdd adds a camera; an empty id is made from the name.
func (a nvrArea) handleCameraAdd(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	req := nvrCameraRequest{Camera: nvr.NewCamera()}
	if !readJSON(w, r, &req) {
		return
	}
	req.tidy()
	c, err := a.nvr.AddCamera(req.Camera)
	if err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, c.ID)
	slog.Info("NVR camera added", "camera", c.ID, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusCreated, c.View(a.nvr.CameraStates()[c.ID]))
}

// handleCameraSet changes a camera; fields left out stay as they are.
func (a nvrArea) handleCameraSet(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	id := r.PathValue("id")
	current, ok := a.nvr.Camera(id)
	if !ok {
		writeNVRError(w, nvr.ErrNotFound)
		return
	}
	// The stored password is never the starting point of an edit: an
	// empty one in the request means "keep" and is decided by the manager.
	current.Source.Password = ""
	req := nvrCameraRequest{Camera: current}
	if !readJSON(w, r, &req) {
		return
	}
	req.tidy()
	c, err := a.nvr.UpdateCamera(id, req.Camera, req.ClearPassword)
	if err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, c.ID)
	slog.Info("NVR camera changed", "camera", c.ID, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, c.View(a.nvr.CameraStates()[c.ID]))
}

// handleCameraRemove removes a camera and the rights on it. Its
// recordings stay until their days are over.
func (a nvrArea) handleCameraRemove(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	id := r.PathValue("id")
	if err := a.nvr.RemoveCamera(id); err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, id)
	slog.Info("NVR camera removed", "camera", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleDiscover searches the home network for ONVIF cameras (three
// seconds, only on this request): {cameras: [{address, onvifPort, name,
// hardware, id, known}]}; known marks addresses that are cameras already.
func (a nvrArea) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), nvr.DiscoverTime+5*time.Second)
	defer cancel()
	found, err := a.nvr.Discover(ctx)
	if err != nil {
		writeNVRError(w, err)
		return
	}
	type entry struct {
		nvr.Found
		Known bool `json:"known"`
	}
	out := []entry{}
	cameras := a.nvr.Cameras()
	for _, f := range found {
		e := entry{Found: f}
		for _, c := range cameras {
			e.Known = e.Known || c.Source.Host == f.Address
		}
		out = append(out, e)
	}
	noteTarget(r, "nvr")
	writeJSON(w, http.StatusOK, map[string]any{"cameras": out})
}

// handleProbe asks one camera before it is added or changed. Two forms:
//
//	{kind: "onvif", host, onvifPort, user, password}: who it is, its
//	profiles and stream addresses, and the two TitanOS would take.
//	{kind: "rtsp", mainURL, subURL, user, password}: whether the address
//	and the login are right, and codec and size of each stream.
//
// camera names a stored camera whose password is used when the request
// has none (and only for that camera's own addresses). allowPublic is the
// administrator's yes to an address outside the home network
// (nvr_target_public). A wrong login is no error of the request: the
// outcome says so.
func (a nvrArea) handleProbe(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var req nvr.ProbeRequest
	if !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	result, err := a.nvr.Probe(ctx, req)
	if err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, "nvr")
	writeJSON(w, http.StatusOK, result)
}

// nvrUserCameras is GET /api/v1/users/{name}/cameras.
type nvrUserCameras struct {
	// Admin: an owner or administrator; sees every camera, nothing to tick.
	Admin   bool            `json:"admin"`
	Cameras []nvrUserCamera `json:"cameras"`
}

type nvrUserCamera struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Live       bool   `json:"live"`
	Recordings bool   `json:"recordings"`
}

func (a nvrArea) userCameras(target auth.User) nvrUserCameras {
	view := nvrUserCameras{Admin: target.Role.CanAdminister(), Cameras: []nvrUserCamera{}}
	if a.nvr == nil {
		return view
	}
	cameras := a.nvr.Cameras()
	rights := a.nvr.Access.User(target.Name, a.nvr.CameraIDs())
	for _, c := range cameras {
		entry := nvrUserCamera{ID: c.ID, Name: c.Name, Live: view.Admin || rights[c.ID].Live, Recordings: view.Admin || rights[c.ID].Recordings}
		view.Cameras = append(view.Cameras, entry)
	}
	return view
}

// handleUserCameras shows which cameras an account may see.
func (a nvrArea) handleUserCameras(w http.ResponseWriter, r *http.Request) {
	target, ok := a.users.Get(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "user_not_found", "no such account")
		return
	}
	writeJSON(w, http.StatusOK, a.userCameras(target))
}

// handleUserCamerasSet changes the cameras of one normal account:
// {cameras: {"haustuer": {live: true, recordings: false}, …}}; cameras
// left out stay as they are. Open live views of the account end within
// a few seconds when their right is gone.
func (a nvrArea) handleUserCamerasSet(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	target, ok := a.users.Get(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "user_not_found", "no such account")
		return
	}
	if target.Role.CanAdminister() {
		writeError(w, http.StatusBadRequest, "nvr_access_admin", "owners and administrators always see every camera")
		return
	}
	var req struct {
		Cameras map[string]nvrCameraRights `json:"cameras"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	rights := nvr.UserRights{}
	for id, want := range req.Cameras {
		rights[id] = struct{ Live, Recordings bool }{want.Live, want.Recordings}
	}
	if err := a.nvr.Access.SetUser(target.Name, rights, a.nvr.CameraIDs()); err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, target.Name)
	slog.Info("camera rights changed", "user", target.Name, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, a.userCameras(target))
}

// nvrForgetUser removes a deleted account from the cameras.
func (a nvrArea) nvrForgetUser(name string) {
	if a.nvr == nil || a.nvr.Access == nil {
		return
	}
	if err := a.nvr.Access.ForgetUser(name); err != nil {
		slog.Error("remove account from the cameras", "user", name, "err", err)
	}
}
