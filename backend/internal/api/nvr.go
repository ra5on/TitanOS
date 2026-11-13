package api

// The NVR (PLAN 79-7, docs/NVR.md): Frigate in a container TitanOS manages,
// its settings and cameras, and who may see which camera. None of these
// routes is in publicRoutes: they are for the web UI only (no tokens, no
// MCP, no agent). Live pictures are in nvr_live.go.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

// nvrArea is the NVR; an area with narrow dependencies (PLAN 8a, see
// fansArea). nvr is nil where there is none; users are the accounts;
// loginHolds tells whether the login of a request still stands (live
// views end with it, like consoles).
type nvrArea struct {
	nvr        *nvr.Manager
	users      *auth.Users
	loginHolds func(r *http.Request, user auth.User) (auth.User, bool)
	// notify and push tell how the recipients of a camera can be reached
	// (e-mail address, mail server, devices); nil reaches nobody.
	notify *notify.Manager
	push   *webpush.Manager
}

// nvrArea is the area of the server.
func (s *Server) nvrArea() nvrArea {
	return nvrArea{nvr: s.opts.NVR, users: s.opts.Users, loginHolds: s.loginHolds, notify: s.opts.Notify, push: s.opts.WebPush}
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
	mux.HandleFunc("DELETE /api/v1/nvr/cameras/{id}/recordings", g.admin(a.handleRemovedPurge))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/snapshot.jpg", g.user(a.handleSnapshot))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/live", g.user(a.handleLive))
	// Next to the apps and shares of an account (Konten und Zugang › Nutzer).
	mux.HandleFunc("GET /api/v1/users/{name}/cameras", g.admin(a.handleUserCameras))
	mux.HandleFunc("PUT /api/v1/users/{name}/cameras", g.admin(a.handleUserCamerasSet))
	// Whom a camera can tell (PLAN 82-3), for the camera's form.
	mux.HandleFunc("GET /api/v1/nvr/recipients", g.admin(a.handleRecipients))
	a.registerEvents(mux, g)
	a.registerRecordings(mux, g)
	a.registerExports(mux, g)
	a.registerPTZ(mux, g)
	a.registerFaces(mux, g)
}

// available answers 404 where there is no NVR.
func (a nvrArea) available(w http.ResponseWriter) bool {
	if a.nvr == nil {
		writeError(w, http.StatusNotFound, "nvr_unavailable", "this NAS has no NVR")
		return false
	}
	return true
}

// reaches tells whether the account may use a right on a camera: one
// that exists, or (owners and administrators, recordings only) a deleted
// one whose recordings are kept.
func (a nvrArea) reaches(user auth.User, camera, right string) bool {
	if _, ok := a.nvr.Camera(camera); ok {
		return a.allows(user, camera, right)
	}
	if right != nvr.RightRecordings || !user.Role.CanAdminister() {
		return false
	}
	_, ok := a.nvr.DeletedCamera(camera)
	return ok && a.allows(user, camera, right)
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
		{nvr.ErrNameExists, http.StatusConflict, "nvr_camera_name_exists", "another camera has this name"},
		{nvr.ErrLimit, http.StatusConflict, "nvr_camera_limit", "no more cameras can be added"},
		{nvr.ErrNotRunning, http.StatusConflict, "nvr_not_running", "the NVR does not run"},
		{nvr.ErrDisabled, http.StatusConflict, "nvr_disabled", "the NVR is switched off"},
		{nvr.ErrRecordingsShrink, http.StatusConflict, "nvr_recordings_shrink", "the change deletes recordings that exist now; confirm it"},
		{nvr.ErrNotLoaded, http.StatusServiceUnavailable, "config_unavailable", "the configuration could not be read from the stick"},
		{nvr.ErrRightsUnreadable, http.StatusServiceUnavailable, "nvr_rights_unreadable", "the camera rights could not be read from the stick; they and the cameras are not changed until a start reads them"},
		{nvr.ErrEventNotFound, http.StatusNotFound, "nvr_event_not_found", "no such event"},
		{nvr.ErrExportNotFound, http.StatusNotFound, "nvr_export_not_found", "no such export"},
		{nvr.ErrExportRunning, http.StatusConflict, "nvr_export_running", "the export is still being made"},
		{nvr.ErrExportFailed, http.StatusConflict, "nvr_export_failed", "the export could not be started; is there a recording in this time?"},
		{nvr.ErrExportsUnreadable, http.StatusServiceUnavailable, "nvr_exports_unreadable", "the owners of the exports could not be read from the NVR's volume"},
		{nvr.ErrPTZUnsupported, http.StatusConflict, "nvr_ptz_unsupported", "this camera cannot be moved"},
		{nvr.ErrRateLimited, http.StatusTooManyRequests, "rate_limited", "too many commands, slow down"},
		{nvr.ErrFaceNotFound, http.StatusNotFound, "nvr_face_not_found", "no such face"},
		{nvr.ErrFaceExists, http.StatusConflict, "nvr_face_exists", "a person of this name exists"},
		{nvr.ErrFaceRejected, http.StatusBadRequest, "nvr_face_rejected", "the picture cannot be used: it must show exactly one face"},
		{nvr.ErrFacesOff, http.StatusConflict, "nvr_faces_off", "no camera recognises faces"},
		{nvr.ErrRecipientUnknown, http.StatusBadRequest, "nvr_recipient_invalid", "a chosen recipient does not exist or is locked"},
		{nvr.ErrRecipientNoRight, http.StatusBadRequest, "nvr_recipient_no_right", "a chosen recipient may not see this camera"},
		{nvr.ErrRecipientBell, http.StatusBadRequest, "nvr_recipient_bell", "only owners and administrators have a bell"},
		{nvr.ErrPurgeRunning, http.StatusConflict, "nvr_purge_running", "the recordings of this camera are being deleted"},
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
			writeJSON(w, http.StatusOK, nvr.Status{State: nvr.PhaseOff, Image: nvr.ImageStatus{State: "missing"}, Hardware: nvr.HardwareInfo{Devices: []nvr.Device{}, Decoders: []nvr.Decoder{}},
				Decoder: nvr.DecoderStatus{Setting: nvr.DecoderAuto, Active: nvr.DecoderCPU, Kind: nvr.DecoderCPU, Title: "CPU"}})
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
	// Plates are the known license plates.
	Plates []nvr.KnownPlate `json:"plates"`
	// RTSPUser and RTSPPassword are the login of the restream on port
	// 8554 ("" while it was never switched on).
	RTSPUser     string `json:"rtspUser"`
	RTSPPassword string `json:"rtspPassword"`
	// NotifyOff silences the messages of every camera (PLAN 82-3).
	NotifyOff bool `json:"notifyOff"`
	// Decoder is the GPU that decodes the streams (PLAN 82-4).
	Decoder string `json:"decoder"`
}

func settingsView(s nvr.Settings) nvrSettingsView {
	view := nvrSettingsView{Enabled: s.Enabled, Volume: s.Volume, Detector: s.Detector, Storage: s.Storage,
		PrivacyNoticeDismissed: s.PrivacyNoticeDismissed, RTSPOut: s.RTSPOut, Plates: s.Plates,
		NotifyOff: s.NotifyOff, Decoder: s.Decoder}
	if view.Decoder == "" {
		view.Decoder = nvr.DecoderAuto
	}
	if view.Plates == nil {
		view.Plates = []nvr.KnownPlate{}
	}
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
// {limitGB}, privacyNoticeDismissed, rtspOut, plates, notifyOff, decoder};
// fields left out stay.
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
	next.PrivacyNoticeDismissed, next.RTSPOut, next.Plates = req.PrivacyNoticeDismissed, req.RTSPOut, req.Plates
	next.NotifyOff, next.Decoder = req.NotifyOff, req.Decoder
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
	// Names: recognised faces' names and plates of this camera (PLAN
	// 82-3); only with Recordings.
	Names bool `json:"names"`
}

func (a nvrArea) brief(user auth.User, c nvr.Camera, state nvr.CameraState) nvrCameraBrief {
	return nvrCameraBrief{ID: c.ID, Name: c.Name, Enabled: c.Enabled, Order: c.Order, Audio: c.Audio, PTZ: c.PTZ, State: state,
		Rights: nvrCameraRights{Live: a.allows(user, c.ID, nvr.RightLive), Recordings: a.allows(user, c.ID, nvr.RightRecordings), Names: a.allows(user, c.ID, nvr.RightNames)}}
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
		// Deleted cameras whose recordings are kept, after the others.
		for _, d := range a.nvr.DeletedCameras() {
			view := d.View()
			view.Purging = a.nvr.Purging(d.ID)
			out = append(out, view)
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
	if !ok && user.Role.CanAdminister() {
		if d, deleted := a.nvr.DeletedCamera(r.PathValue("id")); deleted {
			writeJSON(w, http.StatusOK, d.View())
			return
		}
	}
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
// clearPassword to drop it. confirmShrink answers nvr_recordings_shrink:
// the page told that the change deletes recordings that exist now (a
// narrower recording mode, fewer days, or the ID of a deleted camera with
// less kept than it).
type nvrCameraRequest struct {
	nvr.Camera
	ClearPassword bool `json:"clearPassword"`
	ConfirmShrink bool `json:"confirmShrink"`
	// recipientsNull: the request named the recipients as null.
	recipientsNull bool
}

// UnmarshalJSON decodes a request over the stored camera. Recipients in
// the request replace the stored list as a whole: encoding/json would
// decode them over the stored entries by position, so that an entry left
// partly out took the ways of whoever stood there before.
func (req *nvrCameraRequest) UnmarshalJSON(data []byte) error {
	var probe struct {
		Notify *struct {
			Recipients json.RawMessage `json:"recipients"`
		} `json:"notify"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Notify != nil && probe.Notify.Recipients != nil {
		req.Notify.Recipients = nil
		req.recipientsNull = string(probe.Notify.Recipients) == "null"
	}
	type plain nvrCameraRequest
	return json.Unmarshal(data, (*plain)(req))
}

func (req nvrCameraRequest) options() nvr.CameraOptions {
	return nvr.CameraOptions{ClearPassword: req.ClearPassword, ConfirmShrink: req.ConfirmShrink}
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
	c, err := a.nvr.AddCamera(r.Context(), req.Camera, req.options())
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
	hadList := current.Notify.Recipients != nil
	req := nvrCameraRequest{Camera: current}
	if !readJSON(w, r, &req) {
		return
	}
	// null would turn a camera with recipients back into one of 0.78,
	// whose old ways reach every administrator; nobody is [].
	if hadList && req.recipientsNull {
		writeError(w, http.StatusBadRequest, "nvr_invalid", "notify.recipients: null is not allowed for a camera with recipients; [] is nobody")
		return
	}
	req.tidy()
	c, err := a.nvr.UpdateCamera(r.Context(), id, req.Camera, req.options())
	if err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, c.ID)
	slog.Info("NVR camera changed", "camera", c.ID, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, c.View(a.nvr.CameraStates()[c.ID]))
}

// handleCameraRemove removes a camera and the rights on it. Its
// recordings stay until their days are over (administrators find them
// under the camera, listed as deleted): the deleted camera is noted on
// the stick first, and if that fails the camera stays.
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

// handleRemovedPurge starts deleting the recordings, events and pictures
// of a removed camera now instead of when its days are over (owner
// 13.11.2026); its exports stay. Only for removed cameras: a camera
// that exists answers 404. The NAS deletes in the background (weeks of
// recordings take minutes; a closed window must not stop it): 202, the
// camera is listed with purging: true until it is gone; a failure goes to
// the bell. Frigate restarts once afterwards.
func (a nvrArea) handleRemovedPurge(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	id := r.PathValue("id")
	if err := a.nvr.StartPurge(id); err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, id)
	slog.Info("NVR: deleting a removed camera's recordings", "camera", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
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
	// Names: recognised faces' names and plates (PLAN 82-3).
	Names bool `json:"names"`
}

func (a nvrArea) userCameras(target auth.User) nvrUserCameras {
	view := nvrUserCameras{Admin: target.Role.CanAdminister(), Cameras: []nvrUserCamera{}}
	if a.nvr == nil {
		return view
	}
	cameras := a.nvr.Cameras()
	rights := a.nvr.Access.User(target.Name, a.nvr.CameraIDs())
	for _, c := range cameras {
		entry := nvrUserCamera{ID: c.ID, Name: c.Name, Live: view.Admin || rights[c.ID].Live, Recordings: view.Admin || rights[c.ID].Recordings,
			Names: view.Admin || rights[c.ID].Names}
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
// {cameras: {"haustuer": {live: true, recordings: true, names: false}, …}};
// cameras left out stay as they are. names may be left out (a page of
// 0.78): it then stays as it is; it counts only with recordings. Open
// live views of the account end within a few seconds when their right is
// gone.
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
		Cameras map[string]struct {
			Live       bool  `json:"live"`
			Recordings bool  `json:"recordings"`
			Names      *bool `json:"names"`
		} `json:"cameras"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	current := a.nvr.Access.User(target.Name, a.nvr.CameraIDs())
	rights := nvr.UserRights{}
	for id, want := range req.Cameras {
		names := current[id].Names
		if want.Names != nil {
			names = *want.Names
		}
		rights[id] = nvr.Rights{Live: want.Live, Recordings: want.Recordings, Names: names}
	}
	if err := a.nvr.Access.SetUser(target.Name, rights, a.nvr.CameraIDs()); err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, target.Name)
	slog.Info("camera rights changed", "user", target.Name, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, a.userCameras(target))
}

// nvrForgetUser removes a deleted account from the cameras and from its
// exports (they stay, for administrators).
func (a nvrArea) nvrForgetUser(name string) {
	if a.nvr == nil || a.nvr.Access == nil {
		return
	}
	a.nvr.ForgetExportOwner(name)
	if err := a.nvr.Access.ForgetUser(name); err != nil {
		slog.Error("remove account from the cameras", "user", name, "err", err)
	}
	if err := a.nvr.ForgetRecipient(name); err != nil {
		slog.Error("remove account from the recipients of the cameras", "user", name, "err", err)
	}
}

// nvrRecipient is an account a camera can tell (GET /api/v1/nvr/recipients).
type nvrRecipient struct {
	Name string `json:"name"`
	// Admin: an owner or administrator, the only ones with a bell; they
	// see every camera, plates and faces.
	Admin bool `json:"admin"`
	// Email: the account has an e-mail address of its own (its
	// notification settings); it also needs the mail server (mailServer).
	Email bool `json:"email"`
	// PushDevices are its devices that take camera messages,
	// PushDevicesOff those that switched them off.
	PushDevices    int `json:"pushDevices"`
	PushDevicesOff int `json:"pushDevicesOff"`
	// Cameras are the IDs of the cameras it may see now; Names those whose
	// recognised names and plates it may see (plate and face messages).
	Cameras []string `json:"cameras"`
	Names   []string `json:"names"`
}

// handleRecipients lists the accounts that are not locked, with how they
// can be reached and which cameras they may see: {accounts: […],
// mailServer}. The form of a camera offers those that may see it.
func (a nvrArea) handleRecipients(w http.ResponseWriter, r *http.Request) {
	ids := []string{}
	if a.nvr != nil {
		ids = a.nvr.CameraIDs()
	}
	out := []nvrRecipient{}
	for _, user := range a.users.List() {
		if user.Disabled {
			continue
		}
		entry := nvrRecipient{Name: user.Name, Admin: user.Role.CanAdminister(), Cameras: []string{}, Names: []string{}}
		for _, id := range ids {
			if entry.Admin || a.allows(user, id, nvr.RightLive) || a.allows(user, id, nvr.RightRecordings) {
				entry.Cameras = append(entry.Cameras, id)
			}
			if entry.Admin || a.allows(user, id, nvr.RightNames) {
				entry.Names = append(entry.Names, id)
			}
		}
		if a.notify != nil {
			entry.Email = a.notify.PersonalMail(user.Name)
		}
		if a.push != nil {
			entry.PushDevices, entry.PushDevicesOff = a.push.CameraDevices(user.Name)
		}
		out = append(out, entry)
	}
	slices.SortFunc(out, func(x, y nvrRecipient) int { return strings.Compare(x.Name, y.Name) })
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out, "mailServer": a.notify != nil && a.notify.MailServer()})
}
