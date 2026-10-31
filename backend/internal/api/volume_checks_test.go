package api

import (
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/storage"
)

func TestVolumeCheckJobs(t *testing.T) {
	disks := []storage.Disk{{Name: "sdb", Serial: "S1"}, {Name: "sdc", Serial: "S2"}, {Name: "sdd", Serial: "S3"}}
	all := volumeChecks{Parity: true, SmartShort: true, SmartLong: true, Snapshots: true}
	jobs := volumeCheckJobs(storage.CreateRequest{Name: "tank", Filesystem: "btrfs", Disks: []string{"sdb", "sdc"}}, all, disks)
	kinds := map[string]int{}
	for _, j := range jobs {
		kinds[j.Kind]++
		if !j.Enabled || j.Time == "" || j.Missed != "window" {
			t.Errorf("job %+v", j)
		}
		if j.Kind == "snapshot" && (j.Keep != 14 || !j.AllowPrune || j.Every != "daily") {
			t.Errorf("snapshot %+v", j)
		}
	}
	if kinds["raid-check"] != 1 || kinds["snapshot"] != 1 || kinds["smart-short"] != 2 || kinds["smart-long"] != 2 {
		t.Fatalf("kinds %v", kinds)
	}
	ext4 := volumeCheckJobs(storage.CreateRequest{Name: "data", Filesystem: "ext4", Disks: []string{"sdd"}}, all, disks)
	for _, j := range ext4 {
		if j.Kind == "snapshot" {
			t.Fatal("snapshots on ext4")
		}
	}
	if len(volumeCheckJobs(storage.CreateRequest{Name: "x"}, volumeChecks{}, disks)) != 0 {
		t.Fatal("jobs without checks")
	}
}
