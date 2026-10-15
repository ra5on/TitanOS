package system

import (
	"os"
	"path/filepath"
	"testing"
)

func hwmonFixture(t *testing.T, sys, device, index, chip string) string {
	t.Helper()
	root := filepath.Join(sys, "devices", device, "hwmon", index)
	os.MkdirAll(root, 0o755)
	os.MkdirAll(filepath.Join(sys, "class/hwmon"), 0o755)
	os.Symlink(root, filepath.Join(sys, "class/hwmon", index))
	os.WriteFile(filepath.Join(root, "name"), []byte(chip), 0o600)
	os.WriteFile(filepath.Join(root, "temp1_input"), []byte("38000"), 0o600)
	return root
}

func TestTemperatureIdentitySurvivesHwmonRenumbering(t *testing.T) {
	sys := t.TempDir()
	root := hwmonFixture(t, sys, "platform/coretemp.0", "hwmon5", "coretemp")
	os.WriteFile(filepath.Join(root, "temp1_label"), []byte("Package id 0"), 0o600)
	os.WriteFile(filepath.Join(root, "temp1_crit"), []byte("100000"), 0o600)
	before := readTemperatures(sys)
	if len(before) != 1 || before[0].Label != "Package id 0" || *before[0].Celsius != 38 || *before[0].CriticalC != 100 {
		t.Fatalf("CPU readings: %+v", before)
	}
	newRoot := filepath.Join(filepath.Dir(root), "hwmon9")
	os.Rename(root, newRoot)
	os.Remove(filepath.Join(sys, "class/hwmon/hwmon5"))
	os.Symlink(newRoot, filepath.Join(sys, "class/hwmon/hwmon9"))
	after := readTemperatures(sys)
	if len(after) != 1 || after[0].ID != before[0].ID {
		t.Fatalf("renumbered sensor changed identity: %+v", after)
	}
	hwmonFixture(t, sys, "platform/i2c/0-0050", "hwmon3", "spd5118")
	hwmonFixture(t, sys, "platform/i2c/0-0051", "hwmon4", "spd5118")
	after = readTemperatures(sys)
	if len(after) != 3 || after[1].ID == after[2].ID {
		t.Fatal("identical RAM channel labels collided")
	}
}

func TestTemperatureMissingFaultySleepingAndDiskSensors(t *testing.T) {
	sys := t.TempDir()
	root := hwmonFixture(t, sys, "platform/board", "hwmon0", "nct-test")
	hwmonFixture(t, sys, "pci/nvme", "hwmon1", "nvme")
	hwmonFixture(t, sys, "ata/drive", "hwmon2", "drivetemp")
	for _, value := range []string{"not a number", "", "-999999999"} {
		os.WriteFile(filepath.Join(root, "temp1_input"), []byte(value), 0o600)
		got := readTemperatures(sys)
		if len(got) != 1 || got[0].Celsius != nil || got[0].Label != "temp1" {
			t.Fatalf("invalid/disks/unlabelled: %+v", got)
		}
	}
	os.WriteFile(filepath.Join(root, "temp1_input"), []byte("-5000"), 0o600)
	if got := readTemperatures(sys); *got[0].Celsius != -5 {
		t.Fatal("valid negative temperature rejected")
	}
	os.WriteFile(filepath.Join(root, "temp1_fault"), []byte("1"), 0o600)
	if got := readTemperatures(sys); got[0].Celsius != nil {
		t.Fatal("faulty sensor was displayed")
	}
	os.Remove(filepath.Join(root, "temp1_fault"))
	parent := filepath.Dir(filepath.Dir(root))
	os.MkdirAll(filepath.Join(parent, "power"), 0o755)
	os.WriteFile(filepath.Join(parent, "power/runtime_status"), []byte("suspended"), 0o600)
	if got := readTemperatures(sys); got[0].Celsius != nil {
		t.Fatal("sleeping sensor was read")
	}
	os.Remove(filepath.Join(root, "temp1_input"))
	if got := readTemperatures(sys); len(got) != 0 {
		t.Fatal("disappeared sensor retained a stale value")
	}
}
