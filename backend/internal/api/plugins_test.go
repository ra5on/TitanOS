package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/plugins"
)

func TestPluginUpdateLockIsNotReportedAsBusyHardware(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{plugins.ErrUpdatePending, "plugin_update_pending"},
		{plugins.ErrBusy, "plugin_busy"},
	} {
		response := httptest.NewRecorder()
		writePluginError(response, tc.err)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatalf("%v: %d %s", tc.err, response.Code, response.Body.String())
		}
	}
}

func TestPluginProgressRoutes(t *testing.T) {
	ts := newTestServer(t)
	if res, _ := ts.request("GET", "/api/v1/plugins/progress", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous progress: %d", res.StatusCode)
	}
	ts.setup()
	res, body := ts.request("GET", "/api/v1/plugins/progress", nil)
	if res.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != `{"operations":[]}` {
		t.Fatalf("progress: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("DELETE", "/api/v1/plugins/progress/unknown", nil); res.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "plugin_not_found") {
		t.Fatalf("forget unknown: %d %s", res.StatusCode, body)
	}
	// A refused installation (development mode) adds no operation.
	ts.request("POST", "/api/v1/plugins", map[string]any{"name": "intel-gpu-tools", "volume": "data"})
	res, body = ts.request("GET", "/api/v1/operations", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"plugins":[]`) {
		t.Fatalf("operations without plugin steps: %d %s", res.StatusCode, body)
	}
}
