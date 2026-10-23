package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type smartScheduleRunner struct {
	base        *fakeRunner
	caps        int
	unsupported bool
	calls       []string
}

func (r *smartScheduleRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name != "smartctl" {
		return r.base.Run(ctx, stdin, name, args...)
	}
	r.calls = append(r.calls, strings.Join(args, " "))
	if strings.Contains(strings.Join(args, " "), "--test=long") {
		if r.unsupported {
			return "", errors.New("unsupported")
		}
		return "", nil
	}
	r.caps++
	value := 0
	if r.caps == 2 {
		value = 240
	}
	if value == 240 {
		return `{"ata_smart_data":{"self_test":{"status":{"value":240,"remaining_percent":50}}}}`, nil
	}
	return `{"ata_smart_data":{"self_test":{"status":{"value":0,"passed":true}}}}`, nil
}
func TestScheduledSMARTRequiresObservedStartAndDoesNotClaimUnsupportedSuccess(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		m, base, _ := newTestManager(t)
		runner := &smartScheduleRunner{base: base, unsupported: unsupported}
		m.runner = runner
		m.smartPoll = time.Millisecond
		err := m.LongSMART(context.Background(), "E1")
		if unsupported && err == nil {
			t.Fatal("unsupported SMART passed")
		}
		if !unsupported && err != nil {
			t.Fatal(err)
		}
		tests := m.Tests()
		if len(tests) != 1 {
			t.Fatal(tests)
		}
		if !unsupported && tests[0].Result != TestPassed {
			t.Fatal(tests)
		}
		if unsupported && tests[0].Result != TestFailed {
			t.Fatal(tests)
		}
		for _, cmd := range base.commands {
			if strings.Contains(cmd, "wipe") || strings.Contains(cmd, "sfdisk") {
				t.Fatal("SMART touched disk data", cmd)
			}
		}
	}
}
func TestScheduledSMARTRejectsUnstableOrStickIdentity(t *testing.T) {
	m, _, _ := newTestManager(t)
	for _, serial := range []string{"", "missing", "S1"} {
		if err := m.LongSMART(context.Background(), serial); err == nil {
			t.Fatal("invalid target accepted", serial)
		}
	}
}
