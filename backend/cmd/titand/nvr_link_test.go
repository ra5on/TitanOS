package main

import (
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// PLAN 82-5: a camera's event opens its own page; "Kamera offline" the NVR.
func TestNVRLink(t *testing.T) {
	if got := nvrLink(nvr.Notice{Event: "1731412345.12-ab c&d"}); got != "/nvr/events?event=1731412345.12-ab+c%26d" {
		t.Fatalf("event link %q", got)
	}
	if got := nvrLink(nvr.Notice{Kind: nvr.KindOffline}); got != "" {
		t.Fatalf("offline link %q", got)
	}
}

// Review PLAN 85-4: an account with only the live right gets the live
// view, not an event it cannot open.
func TestCameraPushURL(t *testing.T) {
	rights := map[string]string{"anna": "recordings", "ben": "live"}
	allows := func(user, camera, right string) bool { return camera == "tor" && rights[user] == right }
	event := "/nvr/events?event=1"
	if got := cameraPushURL(event, "anna", "tor", allows); got != event {
		t.Errorf("anna: %q", got)
	}
	if got := cameraPushURL(event, "ben", "tor", allows); got != "/nvr" {
		t.Errorf("ben: %q", got)
	}
	if got := cameraPushURL("", "anna", "tor", allows); got != "/nvr" {
		t.Errorf("no link: %q", got)
	}
}
