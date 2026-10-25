package system

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const memorySettingsFile = "system/memory.json"

var ErrMemoryPercent = errors.New("cache percent must be 10 to 90 in steps of 10")
var ErrMemoryPressure = errors.New("file transfer paused to preserve available memory")

type MemorySettings struct {
	CachePercent int `json:"cachePercent"`
}
type MemoryPolicyStatus struct {
	Settings      MemorySettings `json:"settings"`
	ReadOnly      bool           `json:"readOnly"`
	BudgetBytes   uint64         `json:"budgetBytes"`
	ReserveBytes  uint64         `json:"reserveBytes"`
	InFlightBytes uint64         `json:"inFlightBytes"`
	Memory        Memory         `json:"memory"`
	Paused        bool           `json:"paused"`
	Error         string         `json:"error,omitempty"`
}

// A target for writeback and managed transfers, never a hard limit on Linux's
// entire page cache. Configuration stays in RAM between explicit changes.
type MemoryPolicy struct {
	store                     *config.Store
	memory                    func() Memory
	sysctl                    string
	write                     func(string, []byte) error
	Disabled                  bool
	mu                        sync.Mutex
	settings                  MemorySettings
	loaded                    bool
	budget, reserve, inFlight uint64
	problem                   string
}

func NewMemoryPolicy(store *config.Store, memory func() Memory) *MemoryPolicy {
	return &MemoryPolicy{store: store, memory: memory, sysctl: "/proc/sys/vm", settings: MemorySettings{20}, write: func(p string, b []byte) error { return os.WriteFile(p, b, 0644) }}
}
func memoryReserve(total uint64) uint64 {
	return min(total/4, max(uint64(256<<20), min(uint64(2<<30), total/10)))
}
func memoryBudget(mem Memory, percent int) (uint64, uint64) {
	// Leave headroom for services even if a high percentage was selected.
	reserve := memoryReserve(mem.TotalBytes)
	available := mem.AvailableBytes
	if available <= reserve {
		return 0, reserve
	}
	return (available - reserve) * uint64(percent) / 100, reserve
}
func validMemorySettings(s MemorySettings) bool {
	return s.CachePercent >= 10 && s.CachePercent <= 90 && s.CachePercent%10 == 0
}
func (m *MemoryPolicy) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	settings := MemorySettings{20}
	if err := m.store.ReadJSON(memorySettingsFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.problem = err.Error()
		return err
	}
	if !validMemorySettings(settings) {
		m.problem = ErrMemoryPercent.Error()
		return ErrMemoryPercent
	}
	m.settings = settings
	m.loaded = true
	return m.refreshLocked(true)
}
func (m *MemoryPolicy) Status() MemoryPolicyStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	mem := m.memory()
	budget, reserve := memoryBudget(mem, m.settings.CachePercent)
	return MemoryPolicyStatus{Settings: m.settings, ReadOnly: m.Disabled || !m.loaded, BudgetBytes: budget, ReserveBytes: reserve, InFlightBytes: m.inFlight, Memory: mem, Paused: budget == 0, Error: m.problem}
}

var dirtyKeys = []string{"dirty_bytes", "dirty_background_bytes", "dirty_ratio", "dirty_background_ratio"}

func (m *MemoryPolicy) apply(bytes uint64) (func() error, error) {
	before := map[string]string{}
	for _, key := range dirtyKeys {
		b, err := os.ReadFile(filepath.Join(m.sysctl, key))
		if err != nil {
			return nil, err
		}
		before[key] = strings.TrimSpace(string(b))
	}
	put := func(key, value string) error { return m.write(filepath.Join(m.sysctl, key), []byte(value+"\n")) }
	restore := func() error {
		var result error
		// Writing a byte limit resets the paired ratio and vice versa.
		for _, pair := range [][2]string{{"dirty_bytes", "dirty_ratio"}, {"dirty_background_bytes", "dirty_background_ratio"}} {
			key := pair[0]
			if before[key] == "0" {
				key = pair[1]
			}
			result = errors.Join(result, put(key, before[key]))
		}
		return result
	}
	// A zero byte setting would turn the limit off. Keep a small writeback
	// window while managed transfers wait for the reserve to recover.
	bytes = max(uint64(1<<20), bytes)
	if err := put("dirty_bytes", strconv.FormatUint(bytes, 10)); err != nil {
		return nil, errors.Join(err, restore())
	}
	if err := put("dirty_background_bytes", strconv.FormatUint(max(uint64(4096), bytes/2), 10)); err != nil {
		return nil, errors.Join(err, restore())
	}
	return restore, nil
}
func (m *MemoryPolicy) refreshLocked(force bool) error {
	budget, reserve := memoryBudget(m.memory(), m.settings.CachePercent)
	m.reserve = reserve
	if m.Disabled || !m.loaded {
		m.budget = budget
		return nil
	}
	// Shrink promptly; only grow after at least a 10% change, avoiding jitter.
	if !force && m.problem == "" && budget >= m.budget && budget-m.budget < max(uint64(1<<20), m.budget/10) {
		return nil
	}
	_, err := m.apply(budget)
	if err != nil {
		m.problem = err.Error()
		return err
	}
	m.budget = budget
	m.problem = ""
	return nil
}
func (m *MemoryPolicy) Set(settings MemorySettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disabled || !m.loaded {
		return errors.New("memory settings unavailable")
	}
	if !validMemorySettings(settings) {
		return ErrMemoryPercent
	}
	budget, reserve := memoryBudget(m.memory(), settings.CachePercent)
	rollback, err := m.apply(budget)
	if err != nil {
		m.problem = err.Error()
		return err
	}
	if err = m.store.WriteJSON(memorySettingsFile, settings); err != nil {
		err = errors.Join(err, rollback())
		m.problem = err.Error()
		return err
	}
	m.settings = settings
	m.budget = budget
	m.reserve = reserve
	m.problem = ""
	return nil
}
func (m *MemoryPolicy) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			_ = m.refreshLocked(false)
			m.mu.Unlock()
		}
	}
}

// Acquire shares the budget across all managed writes. A bounded window is
// synced and evicted before its reservation is released by the caller.
func (m *MemoryPolicy) Acquire(ctx context.Context) (int64, func(), error) {
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		m.mu.Lock()
		budget, _ := memoryBudget(m.memory(), m.settings.CachePercent)
		window := min(uint64(8<<20), budget/4)
		window -= window % 4096
		if window >= 4096 && m.inFlight+window <= budget {
			m.inFlight += window
			m.mu.Unlock()
			var once sync.Once
			return int64(window), func() { once.Do(func() { m.mu.Lock(); m.inFlight -= window; m.mu.Unlock() }) }, nil
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case <-deadline.C:
			return 0, nil, ErrMemoryPressure
		case <-time.After(200 * time.Millisecond):
		}
	}
}
