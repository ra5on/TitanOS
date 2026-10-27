package shares

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/quota"
)

type fakeQuota struct {
	mu       sync.Mutex
	projects map[string]uint32
	limits   map[uint32]uint64
	used     map[uint32]uint64
	assigns  int
	failing  error
}

func newFakeQuota() *fakeQuota {
	return &fakeQuota{projects: map[string]uint32{}, limits: map[uint32]uint64{}, used: map[uint32]uint64{}}
}

func (f *fakeQuota) Project(dir string) (uint32, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.projects[dir]
	return id, ok, nil
}
func (f *fakeQuota) Assign(dir string, id uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assigns++
	if f.failing != nil {
		return f.failing
	}
	f.projects[dir] = id
	return nil
}
func (f *fakeQuota) Get(_ string, id uint32) (quota.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return quota.Usage{Used: f.used[id], Limit: f.limits[id]}, nil
}
func (f *fakeQuota) SetLimit(_ string, id uint32, bytes uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limits[id] = bytes
	return nil
}

func waitQuota(t *testing.T, m *Manager, name, state string, vq VolumeQuotas) QuotaStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		m.ApplyQuotas(vq, []string{"data"})
		st, _ := m.Quota(name)
		if st.State == state {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %+v, want %s", name, st, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestShareQuotaLifecycle(t *testing.T) {
	m, _ := newTestManager(t)
	kernel := newFakeQuota()
	m.quotaKernel = kernel
	active, wanted := false, true
	vq := func(string) (string, bool, bool) { return m.mountRoot + "/data", active, wanted }

	if err := m.Create(Share{Name: "klein", Volume: "data", Quota: 1 << 20}, volumes, users); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("tiny quota: %v", err)
	}
	if err := m.Create(Share{Name: "fotos", Volume: "data", Quota: 1 << 30, QuotaID: 7}, volumes, users); err != nil {
		t.Fatal(err)
	}
	if err := m.Create(Share{Name: "frei", Volume: "data"}, volumes, users); err != nil {
		t.Fatal(err)
	}
	fotos := m.List()[0]
	if fotos.QuotaID != firstQuotaID || m.List()[1].QuotaID != 0 {
		t.Fatalf("ids: %+v", m.List())
	}
	os.MkdirAll(m.Path(fotos), 0o755)

	// Without active quotas on the volume the limit waits.
	waitQuota(t, m, "fotos", QuotaPending, vq)
	active = true
	waitQuota(t, m, "fotos", QuotaActive, vq)
	if kernel.limits[firstQuotaID] != 1<<30 || kernel.projects[m.Path(fotos)] != firstQuotaID {
		t.Fatalf("%+v", kernel)
	}

	// The ID stays on update, even if the client sends another one.
	kernel.mu.Lock()
	kernel.used[firstQuotaID] = 512 << 20
	kernel.mu.Unlock()
	if err := m.Update("fotos", Share{Volume: "data", Quota: 2 << 30, QuotaID: 99}, volumes, users); err != nil {
		t.Fatal(err)
	}
	waitQuota(t, m, "fotos", QuotaActive, vq)
	st, _ := m.Quota("fotos")
	if m.List()[0].QuotaID != firstQuotaID || kernel.limits[firstQuotaID] != 2<<30 || st.Used != 512<<20 {
		t.Fatalf("%+v %+v", m.List()[0], st)
	}
	before := kernel.assigns
	m.ApplyQuotas(vq, []string{"data"})
	if kernel.assigns != before {
		t.Fatal("files counted again")
	}

	// Removing the limit keeps the project but lifts the limit.
	m.Update("fotos", Share{Volume: "data"}, volumes, users)
	m.ApplyQuotas(vq, []string{"data"})
	if kernel.limits[firstQuotaID] != 0 {
		t.Fatal("limit kept")
	}
	if _, ok := m.Quota("fotos"); ok {
		t.Fatal("status without limit")
	}

	// A deleted share never hands its ID to a new one, and its limit goes.
	m.Update("fotos", Share{Volume: "data", Quota: 1 << 30}, volumes, users)
	m.ApplyQuotas(vq, []string{"data"})
	m.Delete("fotos")
	m.ApplyQuotas(vq, []string{"data"})
	if kernel.limits[firstQuotaID] != 0 {
		t.Fatal("limit of the deleted share kept")
	}
	m.Create(Share{Name: "neu", Volume: "data", Quota: 1 << 30}, volumes, users)
	reloaded := NewManager(m.store)
	reloaded.Load()
	reloaded.Create(Share{Name: "noch", Volume: "data", Quota: 1 << 30}, volumes, users)
	ids := map[uint32]bool{firstQuotaID: true}
	for _, s := range reloaded.List() {
		if s.Quota > 0 && ids[s.QuotaID] {
			t.Fatalf("ID given twice: %+v", reloaded.List())
		}
		ids[s.QuotaID] = true
	}

	// Switching share quotas off on the volume lifts the limits there.
	wanted = false
	m.ApplyQuotas(vq, []string{"data"})
	for id, limit := range kernel.limits {
		if limit != 0 {
			t.Fatalf("project %d still limited", id)
		}
	}
}

func TestShareQuotaCountingFailureWaits(t *testing.T) {
	m, _ := newTestManager(t)
	kernel := newFakeQuota()
	kernel.failing = errors.New("operation not supported")
	m.quotaKernel = kernel
	vq := func(string) (string, bool, bool) { return m.mountRoot + "/data", true, true }
	m.Create(Share{Name: "fotos", Volume: "data", Quota: 1 << 30}, volumes, users)
	os.MkdirAll(m.Path(m.List()[0]), 0o755)
	st := waitQuota(t, m, "fotos", QuotaError, vq)
	if st.Error == "" {
		t.Fatal("no reason")
	}
	for range 5 {
		m.ApplyQuotas(vq, []string{"data"})
	}
	if kernel.assigns != 1 {
		t.Fatalf("retried %d times at once", kernel.assigns)
	}
}
