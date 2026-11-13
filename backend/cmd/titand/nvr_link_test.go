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
