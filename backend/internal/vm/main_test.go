package vm

import (
	"os"
	"testing"
)

// Tests create disks of many gibibytes in temporary folders. Their checks for
// free space see a fixed amount, not whatever the test machine has left.
func TestMain(m *testing.M) {
	freeBytes = func(path string) (int64, error) {
		if _, err := os.Stat(path); err != nil {
			return 0, err
		}
		return 1 << 40, nil
	}
	os.Exit(m.Run())
}
