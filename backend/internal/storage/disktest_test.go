package storage

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// A small empty disk (a bit more than two chunks, not a multiple of one),
// a disk with a file system, a member of a mounted volume and the stick.
const testDisks = `{"blockdevices": [
 {"name":"/dev/sda","kname":"/dev/sda","type":"disk","size":10498048,"rota":true,"tran":"sata","model":"NEW DISK","serial":"NEW1","fstype":null},
 {"name":"/dev/sdb","kname":"/dev/sdb","type":"disk","size":10498048,"rota":true,"tran":"sata","model":"OLD DISK","serial":"OLD1","fstype":null,"children":[
   {"name":"/dev/sdb1","kname":"/dev/sdb1","type":"part","size":8388608,"fstype":"ntfs","label":"Daten","uuid":"bbbb","mountpoint":null}]},
 {"name":"/dev/sdc","kname":"/dev/sdc","type":"disk","size":10498048,"rota":true,"tran":"sata","model":"BUSY","serial":"BUSY1","fstype":null,"children":[
   {"name":"/dev/sdc1","kname":"/dev/sdc1","type":"part","size":8388608,"fstype":"ext4","uuid":"dddd","mountpoint":"/somewhere"}]},
 {"name":"/dev/sdz","kname":"/dev/sdz","type":"disk","size":2147483648,"rota":true,"rm":true,"tran":"usb","model":"Stick","serial":"S1","fstype":null,"children":[
   {"name":"/dev/sdz1","kname":"/dev/sdz1","type":"part","size":2146435072,"fstype":"vfat","label":"TITANOS","uuid":"cccc","mountpoint":"/efi"}]}
]}`

const (
	smartHealthy   = `{"smart_support":{"available":true},"smart_status":{"passed":true},"ata_smart_attributes":{"table":[{"id":5,"raw":{"value":0}},{"id":197,"raw":{"value":0}}]}}`
	smartWorse     = `{"smart_support":{"available":true},"smart_status":{"passed":true},"ata_smart_attributes":{"table":[{"id":5,"raw":{"value":8}},{"id":197,"raw":{"value":2}}]}}`
	selfRunning    = `{"ata_smart_data":{"self_test":{"status":{"value":249,"string":"in progress, 90% remaining","remaining_percent":90},"polling_minutes":{"extended":100}}}}`
	selfPassed     = `{"ata_smart_data":{"self_test":{"status":{"value":0,"string":"completed without error","passed":true}}}}`
	selfFailed     = `{"ata_smart_data":{"self_test":{"status":{"value":112,"string":"completed: read failure","passed":false}}}}`
	nvmeRunning    = `{"nvme_self_test_log":{"current_self_test_operation":{"value":2},"current_self_test_completion_percent":40}}`
	nvmePassed     = `{"nvme_self_test_log":{"current_self_test_operation":{"value":0},"table":[{"self_test_result":{"value":0,"string":"Completed without error"}}]}}`
	nvmeFailedTest = `{"nvme_self_test_log":{"current_self_test_operation":{"value":0},"table":[{"self_test_result":{"value":7,"string":"Completed: failed segments"}}]}}`
)

// diskRunner answers lsblk and plays a script of smartctl answers.
type diskRunner struct {
	mu       sync.Mutex
	commands []string
	// health is answered to requests for health, in turn (the last one stays);
	// selfTest likewise to requests for the state of the self-test.
	health, selfTest []string
	refuseSelfTest   bool
}

func next(list *[]string) string {
	if len(*list) == 0 {
		return ""
	}
	answer := (*list)[0]
	if len(*list) > 1 {
		*list = (*list)[1:]
	}
	return answer
}

func (r *diskRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	switch {
	case name == "lsblk":
		return testDisks, nil
	case name == "smartctl" && strings.Contains(command, "--test=long"):
		if r.refuseSelfTest {
			return "", &CommandError{Command: command, Err: errors.New("exit status 4")}
		}
		return "{}", nil
	case name == "smartctl" && strings.Contains(command, "--log=selftest"):
		return next(&r.selfTest), nil
	case name == "smartctl" && strings.Contains(command, "--health"):
		return next(&r.health), nil
	}
	return "", nil
}

func (r *diskRunner) ran(part string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, command := range r.commands {
		if strings.Contains(command, part) {
			return true
		}
	}
	return false
}

// memDisk is a disk in memory that can be made to misbehave.
type memDisk struct {
	mu   sync.Mutex
	data []byte
	// failWrite and failRead are offsets at which a chunk fails; corrupt
	// is an offset whose byte reads back different; misplace makes the
	// chunk written at that offset land at offset 0 as well.
	failWrite, failRead, corrupt, misplace int64
	written                                int64
	slow                                   time.Duration
}

func newMemDisk() *memDisk {
	return &memDisk{data: make([]byte, 10498048), failWrite: -1, failRead: -1, corrupt: -1, misplace: -1}
}

func (d *memDisk) WriteAt(p []byte, offset int64) (int, error) {
	time.Sleep(d.slow)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failWrite >= offset && d.failWrite < offset+int64(len(p)) {
		return 0, errors.New("input/output error")
	}
	copy(d.data[offset:], p)
	if d.misplace == offset {
		copy(d.data, p)
	}
	d.written += int64(len(p))
	return len(p), nil
}

func (d *memDisk) ReadAt(p []byte, offset int64) (int, error) {
	time.Sleep(d.slow)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failRead >= offset && d.failRead < offset+int64(len(p)) {
		return 0, errors.New("input/output error")
	}
	copy(p, d.data[offset:])
	if d.corrupt >= offset && d.corrupt < offset+int64(len(p)) {
		p[d.corrupt-offset] ^= 0x01
	}
	return len(p), nil
}

func (d *memDisk) Sync() error  { return nil }
func (d *memDisk) Close() error { return nil }

type testEnv struct {
	m      *Manager
	runner *diskRunner
	disk   *memDisk
	opened []string
	done   chan DiskTest
}

func newDiskTestEnv(t *testing.T) *testEnv {
	t.Helper()
	e := &testEnv{runner: &diskRunner{health: []string{smartHealthy}, selfTest: []string{selfRunning, selfPassed}}, disk: newMemDisk(), done: make(chan DiskTest, 1)}
	e.m = NewManager(config.Open(t.TempDir()))
	e.m.runner, e.m.mountRoot, e.m.sysBlock = e.runner, t.TempDir(), t.TempDir()
	e.m.smartPoll = time.Millisecond
	e.m.openDisk = func(path string, write bool) (blockFile, error) {
		e.opened = append(e.opened, path)
		return e.disk, nil
	}
	e.m.OnTestDone = func(test DiskTest) { e.done <- test }
	if err := e.m.Load(); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *testEnv) run(t *testing.T, disk, mode string) DiskTest {
	t.Helper()
	if err := e.m.StartTest(context.Background(), disk, mode, disk); err != nil {
		t.Fatal(err)
	}
	select {
	case test := <-e.done:
		return test
	case <-time.After(20 * time.Second):
		t.Fatal("the test did not finish")
		return DiskTest{}
	}
}

func kindsOf(test DiskTest) string {
	var kinds []string
	for _, note := range test.Notes {
		kinds = append(kinds, note.Kind)
	}
	return strings.Join(kinds, ",")
}

func TestWriteTestPassesAndLeavesAnEmptyDisk(t *testing.T) {
	e := newDiskTestEnv(t)
	test := e.run(t, "sda", TestWrite)
	if test.Result != TestPassed || test.Errors != 0 || test.Phase != PhaseDone || test.Finished == nil || len(test.Notes) != 0 {
		t.Fatalf("test: %+v", test)
	}
	if len(e.opened) != 1 || e.opened[0] != "/dev/sda" {
		t.Errorf("opened: %v", e.opened)
	}
	// Everything was written (plus the cleared start and end).
	if e.disk.written < int64(len(e.disk.data)) {
		t.Errorf("only %d of %d bytes written", e.disk.written, len(e.disk.data))
	}
	empty := make([]byte, 4096)
	if !bytes.Equal(e.disk.data[:4096], empty) || !bytes.Equal(e.disk.data[len(e.disk.data)-4096:], empty) {
		t.Error("start or end of the disk not cleared")
	}
	if bytes.Equal(e.disk.data[5<<20:5<<20+4096], empty) {
		t.Error("the middle of the disk was not written")
	}
	if !e.runner.ran("smartctl --json=c --test=long /dev/sda") || !e.runner.ran("blockdev --rereadpt /dev/sda") {
		t.Errorf("commands: %v", e.runner.commands)
	}
	if list := e.m.Tests(); len(list) != 1 || list[0].Result != TestPassed {
		t.Errorf("tests: %+v", list)
	}
	if err := e.m.StopTest("sda"); err != nil || len(e.m.Tests()) != 0 {
		t.Errorf("dismissing a finished test: %v", err)
	}
}

func TestReadTestWritesNothing(t *testing.T) {
	e := newDiskTestEnv(t)
	copy(e.disk.data, "precious")
	// A disk with a foreign file system may be read, without confirmation.
	if err := e.m.StartTest(context.Background(), "sdb", TestRead, ""); err != nil {
		t.Fatal(err)
	}
	test := <-e.done
	if test.Result != TestPassed || e.disk.written != 0 || string(e.disk.data[:8]) != "precious" {
		t.Errorf("test: %+v, written %d", test, e.disk.written)
	}
	if e.runner.ran("blockdev") {
		t.Error("the partition table was re-read after a test that only reads")
	}
}

func TestFaultsFailTheTest(t *testing.T) {
	cases := map[string]struct {
		mode   string
		break_ func(e *testEnv)
		notes  string
	}{
		"write error":          {TestWrite, func(e *testEnv) { e.disk.failWrite = 5 << 20 }, "io_errors"},
		"read error":           {TestRead, func(e *testEnv) { e.disk.failRead = 9 << 20 }, "io_errors"},
		"flipped bit":          {TestWrite, func(e *testEnv) { e.disk.corrupt = 10498048 - 100 }, "io_errors"},
		"data in wrong place":  {TestWrite, func(e *testEnv) { e.disk.misplace = 4 << 20 }, "io_errors"},
		"self-test fails":      {TestRead, func(e *testEnv) { e.runner.selfTest = []string{selfRunning, selfFailed} }, "smart_failed"},
		"nvme self-test fails": {TestRead, func(e *testEnv) { e.runner.selfTest = []string{nvmeRunning, nvmeFailedTest} }, "smart_failed"},
		"counters rise":        {TestRead, func(e *testEnv) { e.runner.health = []string{smartHealthy, smartWorse} }, "smart_counters"},
		"health turns bad":     {TestRead, func(e *testEnv) { e.runner.health = []string{smartHealthy, `{"smart_status":{"passed":false}}`} }, "smart_unhealthy"},
	}
	for name, tc := range cases {
		e := newDiskTestEnv(t)
		tc.break_(e)
		test := e.run(t, "sda", tc.mode)
		if test.Result != TestFailed || kindsOf(test) != tc.notes {
			t.Errorf("%s: result %q, notes %q (%+v), errors %d", name, test.Result, kindsOf(test), test.Notes, test.Errors)
		}
	}
}

func TestDisksWithoutSmartStillPass(t *testing.T) {
	// A virtual disk: no health at all.
	e := newDiskTestEnv(t)
	e.runner.health = []string{`{}`}
	if test := e.run(t, "sda", TestRead); test.Result != TestPassed || kindsOf(test) != "smart_unsupported" || e.runner.ran("--test=long") {
		t.Errorf("no SMART: %+v", test)
	}
	// An adapter that reports health but takes no commands.
	e = newDiskTestEnv(t)
	e.runner.refuseSelfTest = true
	if test := e.run(t, "sda", TestRead); test.Result != TestPassed || kindsOf(test) != "smart_unsupported" {
		t.Errorf("self-test refused: %+v", test)
	}
	// NVMe reports its self-test differently.
	e = newDiskTestEnv(t)
	e.runner.selfTest = []string{nvmeRunning, nvmePassed}
	if test := e.run(t, "sda", TestRead); test.Result != TestPassed || len(test.Notes) != 0 {
		t.Errorf("nvme: %+v", test)
	}
}

func TestOnlyDisksThatNothingUsesAreTested(t *testing.T) {
	e := newDiskTestEnv(t)
	ctx := context.Background()
	for name, disk := range map[string]string{"mounted": "sdc", "the stick": "sdz", "unknown": "sdq"} {
		if err := e.m.StartTest(ctx, disk, TestRead, ""); !errors.Is(err, ErrDiskUnavailable) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := e.m.StartTest(ctx, "sda", TestWrite, ""); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("write without the name: %v", err)
	}
	if err := e.m.StartTest(ctx, "sda", "format", "sda"); !errors.Is(err, ErrNotPossible) {
		t.Errorf("unknown mode: %v", err)
	}
	e.m.Claimed = func() map[string]string { return map[string]string{"sda": "VM win11"} }
	if err := e.m.StartTest(ctx, "sda", TestWrite, "sda"); !errors.Is(err, ErrDiskUnavailable) || !strings.Contains(err.Error(), "win11") {
		t.Errorf("a disk of a VM: %v", err)
	}
	// Nor does such a disk go into a volume.
	status, _ := e.m.Status(ctx)
	if _, err := e.m.usable(status, "sda"); !errors.Is(err, ErrDiskUnavailable) {
		t.Errorf("a volume could take the disk of a VM: %v", err)
	}
	e.m.Claimed = nil
	e.m.ReadOnly = true
	if err := e.m.StartTest(ctx, "sda", TestRead, ""); !errors.Is(err, ErrReadOnly) {
		t.Errorf("development mode: %v", err)
	}
	if len(e.opened) != 0 {
		t.Errorf("a disk was opened: %v", e.opened)
	}
}

func TestADiskUnderTestIsTakenAndCanBeCancelled(t *testing.T) {
	e := newDiskTestEnv(t)
	e.disk.slow = 20 * time.Millisecond
	ctx := context.Background()
	if err := e.m.StartTest(ctx, "sda", TestWrite, "sda"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.StartTest(ctx, "sda", TestRead, ""); !errors.Is(err, ErrTestRunning) {
		t.Errorf("second test of the same disk: %v", err)
	}
	status, _ := e.m.Status(ctx)
	if !status.Disks[0].Testing {
		t.Error("the disk is not marked as being tested")
	}
	if _, err := usable(status, "sda"); !errors.Is(err, ErrDiskUnavailable) {
		t.Errorf("a volume could take the disk: %v", err)
	}
	if err := e.m.StopTest("sda"); err != nil {
		t.Fatal(err)
	}
	test := <-e.done
	if test.Result != TestCancelled {
		t.Errorf("result: %q", test.Result)
	}
	// What was begun is not left looking like something.
	if !bytes.Equal(e.disk.data[:4096], make([]byte, 4096)) {
		t.Error("the start of the disk was not cleared after cancelling")
	}
	if status, _ := e.m.Status(ctx); status.Disks[0].Testing {
		t.Error("still marked as being tested")
	}
	if err := e.m.StopTest("sdq"); !errors.Is(err, ErrNoTest) {
		t.Errorf("stopping a test that is not there: %v", err)
	}
}
