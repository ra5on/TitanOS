package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/docker"
)

var dockerResourceRoutes = [][2]string{
	{"GET", "/api/v1/docker/images"},
	{"DELETE", "/api/v1/docker/images/sha256:" + "aaaaaaaaaaaa"},
	{"POST", "/api/v1/docker/images/prune"},
	{"GET", "/api/v1/docker/volumes"},
	{"DELETE", "/api/v1/docker/volumes/old_data"},
	{"POST", "/api/v1/docker/volumes/prune"},
	{"POST", "/api/v1/docker/builder/prune"},
	{"GET", "/api/v1/docker/df"},
	{"GET", "/api/v1/docker/networks/all"},
	{"POST", "/api/v1/docker/networks/bridge"},
	{"DELETE", "/api/v1/docker/networks/bridge/spare"},
	{"POST", "/api/v1/docker/networks/prune"},
}

func TestDockerResourcesAreForAdministrators(t *testing.T) {
	ts := newTestServer(t)
	for _, tc := range dockerResourceRoutes {
		if res, _ := ts.do(ts.newClient(), tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous: %d", tc[0], tc[1], res.StatusCode)
		}
	}
	ts.setup()
	// Development mode: the engine is never touched.
	for _, tc := range dockerResourceRoutes {
		if res, body := ts.request(tc[0], tc[1], map[string]any{"name": "spare"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
			t.Errorf("%s %s in development mode: %d %s", tc[0], tc[1], res.StatusCode, body)
		}
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range dockerResourceRoutes {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
}

func TestDockerResourceErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		kind   string
		err    error
		status int
		code   string
	}{
		{"image", fmt.Errorf("%w: conflict", docker.ErrInUse), 409, "image_in_use"},
		{"volume", docker.ErrInUse, 409, "volume_in_use"},
		{"image", docker.ErrProtected, 409, "image_protected"},
		{"volume", docker.ErrProtected, 409, "volume_protected"},
		{"image", fmt.Errorf("%w: setting up", docker.ErrBusy), 409, "app_busy"},
		{"image", docker.ErrNotRunning, 409, "docker_not_running"},
		{"volume", docker.ErrNotFound, 404, "docker_not_found"},
		{"image", errors.New("boom"), 500, "docker_failed"},
		{"network", docker.ErrInUse, 409, "network_in_use"},
		{"network", docker.ErrProtected, 409, "network_protected"},
	} {
		w := httptest.NewRecorder()
		writeResourceError(w, tc.kind, tc.err)
		if w.Code != tc.status || errorCode(t, w.Body.Bytes()) != tc.code {
			t.Errorf("%v: %d %s", tc.err, w.Code, w.Body)
		}
	}
}

func TestBridgeErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("%w: 10.0.0.0/8", docker.ErrOverlap), 409, "network_overlap"},
		{docker.ErrExists, 409, "network_exists"},
		{fmt.Errorf("%w: name", docker.ErrInvalid), 400, "invalid_network"},
		{docker.ErrProtected, 409, "network_protected"},
		{docker.ErrInUse, 409, "network_in_use"},
		{docker.ErrDisabled, 409, "read_only"},
	} {
		w := httptest.NewRecorder()
		writeBridgeError(w, tc.err)
		if w.Code != tc.status || errorCode(t, w.Body.Bytes()) != tc.code {
			t.Errorf("%v: %d %s", tc.err, w.Code, w.Body)
		}
	}
}
