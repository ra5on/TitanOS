package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestHealthReportRoutes(t *testing.T) {
	ts := newTestServer(t)
	if res, _ := ts.request("GET", "/api/v1/health-report", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}
	ts.setup()
	res, body := ts.request("GET", "/api/v1/health-report", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"enabled":false`) || !strings.Contains(string(body), `"preview":{`) || strings.Contains(string(body), `"next"`) {
		t.Fatalf("get: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/health-report", map[string]any{"enabled": true, "every": "hourly", "weekday": 1, "day": 1, "time": "08:00"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid: %d", res.StatusCode)
	}
	if res, body := ts.request("PUT", "/api/v1/health-report", map[string]any{"enabled": true, "every": "monthly", "weekday": 0, "day": 3, "time": "07:15"}); res.StatusCode != 200 || !strings.Contains(string(body), `"next"`) {
		t.Fatalf("set: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/health-report/send", map[string]any{}); res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "no_notify_channel") {
		t.Fatalf("send without channel: %d %s", res.StatusCode, body)
	}
}
