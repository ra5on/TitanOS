package api

import (
	"net/http"
	"testing"
)

func TestAccountNotificationsNeedLogin(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	res, _ := ts.do(&http.Client{}, "GET", "/api/v1/account/notifications", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without login: %d", res.StatusCode)
	}
}
