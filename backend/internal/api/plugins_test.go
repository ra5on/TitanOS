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
