package auth

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func processFactorUsers(t *testing.T) (*Users, []string) {
	t.Helper()
	users := NewUsers(config.Open(t.TempDir()))
	if err := users.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := users.CreateOwner("alex", "a long recovery test password"); err != nil {
		t.Fatal(err)
	}
	secret, _, err := users.BeginTwoFactor("alex", "test")
	if err != nil {
		t.Fatal(err)
	}
	codes, err := users.EnableTwoFactor("alex", CodeForTest(secret, 0))
	if err != nil {
		t.Fatal(err)
	}
	return users, codes
}

func TestRecoveryCodeSharedManagersOnce(t *testing.T) {
	web, codes := processFactorUsers(t)
	console := NewUsers(config.Open(web.store.Dir()))
	if err := console.Load(); err != nil {
		t.Fatal(err)
	}
	if !console.CheckSecondFactor("alex", codes[0]) {
		t.Fatal("fresh console rejected the recovery code")
	}
	if web.CheckSecondFactor("alex", codes[0]) {
		t.Fatal("web daemon accepted a code already consumed by the console")
	}
}

func TestSecondFactorCrossProcess(t *testing.T) {
	if dir := os.Getenv("TITANOS_AUTH_PROBE_DIR"); dir != "" {
		users := NewUsers(config.Open(dir))
		if err := users.Load(); err != nil {
			t.Fatal(err)
		}
		if !users.CheckSecondFactor("alex", os.Getenv("TITANOS_AUTH_PROBE_CODE")) {
			t.Fatal("child could not consume its fresh factor")
		}
		return
	}
	for _, kind := range []string{"recovery", "totp"} {
		t.Run(kind, func(t *testing.T) {
			web, codes := processFactorUsers(t)
			code := codes[0]
			if kind == "totp" {
				user, _ := web.Get("alex")
				code = CodeForTest(user.TOTPSecret, 1)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestSecondFactorCrossProcess$")
			child.Env = append(os.Environ(), "TITANOS_AUTH_PROBE_DIR="+web.store.Dir(), "TITANOS_AUTH_PROBE_CODE="+code)
			if output, err := child.CombinedOutput(); err != nil {
				t.Fatalf("child: %v: %s", err, output)
			}
			if web.CheckSecondFactor("alex", code) {
				t.Fatal("already-running daemon accepted a factor used by another process")
			}
			restarted := NewUsers(config.Open(web.store.Dir()))
			if err := restarted.Load(); err != nil {
				t.Fatal(err)
			}
			if restarted.CheckSecondFactor("alex", code) {
				t.Fatal("daemon restart made a consumed factor reusable")
			}
		})
	}
}

func TestRecoveryCodeNotResurrectedByStaleAccountUpdate(t *testing.T) {
	web, codes := processFactorUsers(t)
	console := NewUsers(config.Open(web.store.Dir()))
	if err := console.Load(); err != nil {
		t.Fatal(err)
	}
	if !console.CheckSecondFactor("alex", codes[0]) {
		t.Fatal("console factor rejected")
	}
	if _, err := web.SetPassword("alex", "another long recovery password"); err != nil {
		t.Fatal(err)
	}
	fresh := NewUsers(config.Open(web.store.Dir()))
	if err := fresh.Load(); err != nil {
		t.Fatal(err)
	}
	if fresh.CheckSecondFactor("alex", codes[0]) || !fresh.CheckSecondFactor("alex", codes[1]) {
		t.Fatal("stale account update restored a used code or lost an unused one")
	}
}

func TestConcurrentRecoveryConsumption(t *testing.T) {
	web, codes := processFactorUsers(t)
	var managers []*Users
	for range 8 {
		users := NewUsers(config.Open(web.store.Dir()))
		if err := users.Load(); err != nil {
			t.Fatal(err)
		}
		managers = append(managers, users)
	}
	var accepted atomic.Int32
	var running sync.WaitGroup
	for _, users := range managers {
		running.Add(1)
		go func() {
			defer running.Done()
			if users.CheckSecondFactor("alex", codes[0]) {
				accepted.Add(1)
			}
		}()
	}
	running.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("factor accepted %d times", accepted.Load())
	}
}

func TestFactorPersistenceFailureDeniesAuthentication(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("run without root to exercise an unwritable account directory")
	}
	for _, kind := range []string{"recovery", "totp"} {
		t.Run(kind, func(t *testing.T) {
			users, codes := processFactorUsers(t)
			code := codes[0]
			if kind == "totp" {
				user, _ := users.Get("alex")
				code = CodeForTest(user.TOTPSecret, 1)
			}
			dir := filepath.Join(users.store.Dir(), "users/alex")
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(dir, 0o700) })
			if users.CheckSecondFactor("alex", code) {
				t.Fatal("authentication passed without persisting factor consumption")
			}
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if !users.CheckSecondFactor("alex", code) {
				t.Fatal("failed persistence consumed the usable factor")
			}
		})
	}
}
