// Package monitoring records bounded, optional minute samples on a data volume.
package monitoring

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const settingsFile = "system/monitoring.json"

type Settings struct {
	Enabled bool   `json:"enabled"`
	Volume  string `json:"volume"`
	Days    int    `json:"days"`
	MaxMiB  int    `json:"maxMiB"`
}
type Disk struct {
	Name      string   `json:"name"`
	ReadBPS   float64  `json:"readBps"`
	WriteBPS  float64  `json:"writeBps"`
	LatencyMS *float64 `json:"latencyMs"`
}
type Volume struct {
	Name  string `json:"name"`
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}
type Network struct {
	Name    string  `json:"name"`
	Receive float64 `json:"receive"`
	Send    float64 `json:"send"`
}
type Sample struct {
	At           time.Time `json:"at"`
	Gap          bool      `json:"gap"`
	CPU          float64   `json:"cpu"`
	RAMTotal     uint64    `json:"ramTotal"`
	RAMAvailable uint64    `json:"ramAvailable"`
	Disks        []Disk    `json:"disks"`
	Volumes      []Volume  `json:"volumes"`
	Networks     []Network `json:"networks"`
}
type Status struct {
	Settings Settings   `json:"settings"`
	Last     *time.Time `json:"last"`
	Error    string     `json:"error"`
}
type counters struct {
	at     time.Time
	values [5]uint64
}
type Manager struct {
	store         *config.Store
	MountRoot     string
	Mounted       func() []string
	Snapshot      func() (system.Info, storage.Status)
	mu            sync.Mutex
	ioMu          sync.Mutex
	settings      Settings
	last          *time.Time
	problem       string
	previous      map[string]counters
	cachedSamples []Sample
	warmed        bool
	sysBlock      string
}

func New(store *config.Store) *Manager {
	return &Manager{store: store, MountRoot: "/mnt", sysBlock: "/sys/block", settings: Settings{Days: 30, MaxMiB: 256}, previous: map[string]counters{}}
}
func (m *Manager) Load() error {
	var s Settings
	if err := m.store.ReadJSON(settingsFile, &s); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := validate(s); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = s
	m.mu.Unlock()
	return nil
}
func validate(s Settings) error {
	if (s.Enabled && !naming.ValidVolume(s.Volume)) || (s.Days != 7 && s.Days != 30) || s.MaxMiB < 8 || s.MaxMiB > 256 {
		return errors.New("invalid monitoring settings")
	}
	return nil
}
func (m *Manager) Set(s Settings) error {
	if err := validate(s); err != nil {
		return err
	}
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = s
	m.previous = map[string]counters{}
	m.cachedSamples = nil
	m.warmed = false
	m.mu.Unlock()
	return nil
}
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Status{m.settings, m.last, m.problem}
}
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := m.Record(now); err != nil {
				m.mu.Lock()
				m.problem = err.Error()
				m.mu.Unlock()
			} else {
				m.mu.Lock()
				volume := m.settings.Volume
				warm := !m.warmed && m.settings.Enabled
				m.warmed = m.warmed || warm
				m.mu.Unlock()
				if warm {
					if history, err := m.History(ctx, 168); err == nil {
						m.remember(history, volume)
					}
				}
			}
		}
	}
}
func (m *Manager) Record(now time.Time) error {
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	s := m.Status().Settings
	if !s.Enabled {
		return nil
	}
	if m.Mounted == nil || m.Snapshot == nil {
		return volumedata.ErrUnavailable
	}
	root, err := volumedata.Open(m.MountRoot, s.Volume, "monitoring", m.Mounted())
	if err != nil {
		return err
	}
	defer root.Close()
	info, status := m.Snapshot()
	sample := Sample{At: now.UTC(), CPU: info.CPU.UsagePercent, RAMTotal: info.Memory.TotalBytes, RAMAvailable: info.Memory.AvailableBytes, Disks: []Disk{}, Volumes: []Volume{}, Networks: []Network{}}
	for _, v := range status.Volumes {
		if v.Mounted {
			sample.Volumes = append(sample.Volumes, Volume{v.Name, v.SizeBytes, v.UsedBytes})
		}
	}
	for _, n := range info.Networks {
		sample.Networks = append(sample.Networks, Network{n.Interface, n.RxBytesPerSecond, n.TxBytesPerSecond})
	}
	entries, err := os.ReadDir(m.sysBlock)
	if err != nil {
		return err
	}
	m.mu.Lock()
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "loop") || strings.HasPrefix(e.Name(), "ram") || strings.HasPrefix(e.Name(), "zram") || strings.HasPrefix(e.Name(), "md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.sysBlock, e.Name(), "stat"))
		if err != nil {
			continue
		}
		fields := strings.Fields(string(data))
		if len(fields) < 8 {
			continue
		}
		var c [5]uint64
		valid := true
		for i, j := range []int{0, 4, 2, 6, 3} {
			c[i], err = strconv.ParseUint(fields[j], 10, 64)
			valid = valid && err == nil
		}
		writeMS, e2 := strconv.ParseUint(fields[7], 10, 64)
		valid = valid && e2 == nil
		c[4] += writeMS
		if !valid {
			continue
		}
		previous, exists := m.previous[e.Name()]
		m.previous[e.Name()] = counters{now, c}
		seconds := now.Sub(previous.at).Seconds()
		if !exists || seconds <= 0 || seconds > 150 {
			continue
		}
		for i := range c {
			if c[i] < previous.values[i] {
				valid = false
			}
		}
		if !valid {
			continue
		}
		d := Disk{Name: e.Name(), ReadBPS: float64(c[2]-previous.values[2]) * 512 / seconds, WriteBPS: float64(c[3]-previous.values[3]) * 512 / seconds}
		ops := c[0] - previous.values[0] + c[1] - previous.values[1]
		if ops > 0 {
			latency := float64(c[4]-previous.values[4]) / float64(ops)
			d.LatencyMS = &latency
		}
		sample.Disks = append(sample.Disks, d)
	}
	m.mu.Unlock()
	data, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	if len(data) > 60000 {
		return errors.New("monitoring sample exceeds limit")
	}
	name := now.UTC().Format("2006-01-02") + ".jsonl"
	if err := prune(root, s, now, int64(len(data)+1)); err != nil {
		return err
	}
	if st, e := root.Lstat(name); e == nil && !st.Mode().IsRegular() {
		return errors.New("invalid monitoring file")
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err = repairTail(file); err == nil {
		_, err = file.Write(append(data, '\n'))
	}
	if err == nil {
		err = file.Sync()
	}
	ce := file.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	m.mu.Lock()
	stamp := now.UTC()
	m.last = &stamp
	m.problem = ""
	m.mu.Unlock()
	m.remember([]Sample{sample}, s.Volume)
	return nil
}
func records(root *os.Root) ([]fs.DirEntry, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	list, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	list = slices.DeleteFunc(list, func(e fs.DirEntry) bool { _, err := time.Parse("2006-01-02.jsonl", e.Name()); return err != nil })
	sort.Slice(list, func(i, j int) bool { return list[i].Name() < list[j].Name() })
	return list, nil
}
func prune(root *os.Root, s Settings, now time.Time, reserve int64) error {
	list, err := records(root)
	if err != nil {
		return err
	}
	cutoff := now.UTC().AddDate(0, 0, -s.Days).Format("2006-01-02")
	var total int64
	var keep []fs.DirEntry
	for _, e := range list {
		st, err := e.Info()
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return errors.New("invalid monitoring entry")
		}
		if e.Name()[:10] < cutoff {
			if err := root.Remove(e.Name()); err != nil {
				return err
			}
		} else {
			total += st.Size()
			keep = append(keep, e)
		}
	}
	for total+reserve > int64(s.MaxMiB)<<20 && len(keep) > 0 {
		if keep[0].Name() == now.UTC().Format("2006-01-02")+".jsonl" {
			return errors.New("monitoring budget exhausted")
		}
		st, err := keep[0].Info()
		if err != nil {
			return err
		}
		if err := root.Remove(keep[0].Name()); err != nil {
			return err
		}
		total -= st.Size()
		keep = keep[1:]
	}
	return nil
}
func (m *Manager) History(ctx context.Context, hours int) ([]Sample, error) {
	if hours != 24 && hours != 168 && hours != 720 {
		return nil, errors.New("invalid history range")
	}
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	s := m.Status().Settings
	if !s.Enabled {
		return []Sample{}, nil
	}
	if m.Mounted == nil {
		return nil, volumedata.ErrUnavailable
	}
	root, err := volumedata.Open(m.MountRoot, s.Volume, "monitoring", m.Mounted())
	if err != nil {
		return nil, err
	}
	defer root.Close()
	list, err := records(root)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	samples := []Sample{}
	var read int64
	for _, e := range list {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if e.Name()[:10] < cutoff.Format("2006-01-02") {
			continue
		}
		st, err := e.Info()
		if err != nil {
			return nil, err
		}
		read += st.Size()
		if !st.Mode().IsRegular() || read > int64(s.MaxMiB)<<20 {
			return nil, fmt.Errorf("monitoring data exceeds budget")
		}
		file, err := root.OpenFile(e.Name(), os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err := repairTail(file); err != nil {
			file.Close()
			return nil, err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			file.Close()
			return nil, err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 65536)
		for scanner.Scan() {
			var sample Sample
			if err := json.Unmarshal(scanner.Bytes(), &sample); err != nil {
				file.Close()
				return nil, err
			}
			if !sample.At.Before(cutoff) {
				samples = append(samples, sample)
			}
		}
		err = scanner.Err()
		file.Close()
		if err != nil {
			return nil, err
		}
	}
	for i := 1; i < len(samples); i++ {
		samples[i].Gap = samples[i].At.Sub(samples[i-1].At) > 150*time.Second
	}
	// Retain explicit observation gaps while bounding the response.
	if len(samples) > 1200 {
		step := (len(samples) + 799) / 800
		out := []Sample{}
		for i, sample := range samples {
			if i%step == 0 || i == len(samples)-1 || sample.Gap || (i+1 < len(samples) && samples[i+1].Gap) {
				out = append(out, sample)
			}
		}
		if len(out) > 1200 {
			return nil, errors.New("too many observation gaps; choose a shorter history range")
		}
		samples = out
	}
	return samples, nil
}

// A crash may leave only the last append incomplete. Never hide malformed complete records.
func repairTail(file *os.File) error {
	st, err := file.Stat()
	if err != nil || st.Size() == 0 {
		return err
	}
	n := min(st.Size(), int64(65536))
	b := make([]byte, n)
	if _, err := file.ReadAt(b, st.Size()-n); err != nil {
		return err
	}
	if b[len(b)-1] == '\n' {
		return nil
	}
	i := bytes.LastIndexByte(b, '\n')
	if i < 0 && st.Size() > n {
		return errors.New("invalid monitoring tail")
	}
	return file.Truncate(st.Size() - n + int64(i+1))
}
