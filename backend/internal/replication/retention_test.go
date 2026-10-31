package replication

import (
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/retention"
)

func TestReceivedSnapshotsKeptByRules(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local)
	var all []string
	for d := 20; d >= 0; d-- {
		all = append(all, now.AddDate(0, 0, -d).UTC().Format("20060102T150405Z"))
	}
	// Without rules (or from a sender before 0.41.0): the newest keep stay.
	if gone := keepReceived(all, 5, "", now); len(gone) != 16 || gone[0] != all[0] {
		t.Fatalf("count: %v", gone)
	}
	gone := keepReceived(all, 5, `{"daily":3,"weekly":2,"monthly":0}`, now)
	stays := slices.DeleteFunc(slices.Clone(all), func(n string) bool { return slices.Contains(gone, n) })
	if len(stays) != 4 || stays[len(stays)-1] != all[len(all)-1] {
		t.Fatalf("rules kept %v", stays)
	}
	// Rules a receiver does not accept fall back to the count.
	if gone := keepReceived(all, 5, `{"mode":"rolling"}`, now); len(gone) != 16 {
		t.Fatalf("bad rules: %d gone", len(gone))
	}
	job := Job{URL: "https://nas:8750/src", Keep: 9, Versions: &retention.Keep{Daily: 7, WeeklyDay: 7}}
	address := replicaURL(job, "x")
	if !strings.Contains(address, "keep=9") {
		t.Fatal(address)
	}
	u, _ := url.Parse(address)
	if !strings.Contains(u.Query().Get("versions"), `"weeklyDay":7`) {
		t.Fatal(address)
	}
}
