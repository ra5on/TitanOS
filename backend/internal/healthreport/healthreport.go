// Package healthreport sends a regular summary of how the NAS is doing
// (docs/HEALTH-REPORT.md): volumes, disks, backups, updates, UPS and the
// messages since the last report, weekly or monthly through the channels of
// Settings › Notifications. Off unless switched on. Everything comes from
// what titand already holds in RAM; making a report wakes no disk.
package healthreport

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const settingsFile = "system/health-report.json"

var (
	ErrInvalid = errors.New("health report: invalid settings")
	clock      = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
)

// Settings are kept in /config/system/health-report.json.
type Settings struct {
	Enabled bool `json:"enabled"`
	// Every is "weekly" or "monthly".
	Every string `json:"every"`
	// Weekday (0 = Sunday) for weekly, Day (1–28) for monthly reports.
	Weekday int    `json:"weekday"`
	Day     int    `json:"day"`
	Time    string `json:"time"`
}

func defaults() Settings {
	return Settings{Every: "weekly", Weekday: 1, Day: 1, Time: "08:00"}
}

func (s Settings) check() error {
	if s.Every != "weekly" && s.Every != "monthly" || s.Weekday < 0 || s.Weekday > 6 || s.Day < 1 || s.Day > 28 || !clock.MatchString(s.Time) {
		return ErrInvalid
	}
	return nil
}

// Data is what a report is made of, gathered by titand from RAM.
type Data struct {
	Version string
	Uptime  time.Duration
	// MemoryPercent is the RAM in use (MemTotal − MemAvailable); -1 unknown.
	MemoryPercent int
	// CPUWatts is what the CPU packages draw now (RAPL); nil unknown.
	CPUWatts *float64
	Volumes  []Volume
	Disks    []Disk
	Backups  []Backup
	// UpdateAvailable is the version of a newer release, if one was found.
	UpdateAvailable string
	// UPS is nil without one.
	UPS *UPS
	// Warnings and Errors count the messages of the bell since Since.
	Warnings, Errors int
	Since            time.Time
}

type Volume struct {
	Name, State string
	Mounted     bool
	// Locked is an encrypted volume that waits for its password.
	Locked     bool
	Size, Used uint64
}

type Disk struct {
	Name, Model, Volume string
	// Healthy is nil when the disk reports no SMART.
	Healthy  *bool
	TempC    int
	Warnings int
}

type Backup struct {
	Name      string
	Enabled   bool
	LastOK    *time.Time
	LastError string
	Overdue   bool
}

type UPS struct {
	Connected, OnBattery bool
	Charge               int
}

// Status of a section and of the whole report.
const (
	OK      = "ok"
	Warning = "warning"
	Error   = "error"
)

// Section is one part of the report.
type Section struct {
	Title  string   `json:"title"`
	Status string   `json:"status"`
	Lines  []string `json:"lines"`
}

// Report is a finished summary.
type Report struct {
	Generated time.Time `json:"generated"`
	Status    string    `json:"status"`
	Title     string    `json:"title"`
	Sections  []Section `json:"sections"`
}

// Text is the report as plain text, for e-mail, ntfy and the like.
func (r Report) Text() string {
	var b strings.Builder
	for i, s := range r.Sections {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s %s\n", mark(s.Status), s.Title)
		for _, line := range s.Lines {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	return b.String()
}

func mark(status string) string {
	switch status {
	case Error:
		return "✖"
	case Warning:
		return "⚠"
	}
	return "✔"
}

func worst(a, b string) string {
	rank := map[string]int{OK: 0, Warning: 1, Error: 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// Build turns data into a report in the language of the notifications.
func Build(d Data, language string, now time.Time) Report {
	w := words[language]
	if w == nil {
		w = words["en"]
	}
	r := Report{Generated: now, Status: OK}
	add := func(s Section) {
		r.Status = worst(r.Status, s.Status)
		r.Sections = append(r.Sections, s)
	}

	vol := Section{Title: w["volumes"], Status: OK}
	if len(d.Volumes) == 0 {
		vol.Lines = append(vol.Lines, w["noVolumes"])
	}
	for _, v := range d.Volumes {
		status := OK
		switch {
		case v.Locked:
			status = Warning
		case v.State == "failed" || v.State == "missing" || !v.Mounted:
			status = Error
		case v.State != "ok" && v.State != "checking":
			status = Warning
		}
		line := fmt.Sprintf("%s: %s", v.Name, stateWord(w, v))
		if v.Size > 0 {
			percent := int(v.Used * 100 / v.Size)
			line += fmt.Sprintf(" · %d %% %s (%s / %s)", percent, w["used"], bytes(v.Used), bytes(v.Size))
			if percent >= 90 {
				status = worst(status, Warning)
			}
		}
		vol.Status = worst(vol.Status, status)
		vol.Lines = append(vol.Lines, line)
	}
	add(vol)

	disk := Section{Title: w["disks"], Status: OK}
	hottest, unhealthy := 0, 0
	for _, x := range d.Disks {
		if x.Healthy != nil && !*x.Healthy {
			unhealthy++
			disk.Status = Error
			disk.Lines = append(disk.Lines, fmt.Sprintf("%s (%s): %s", x.Name, x.Model, w["smartFailed"]))
		} else if x.Warnings > 0 {
			disk.Status = worst(disk.Status, Warning)
			disk.Lines = append(disk.Lines, fmt.Sprintf("%s (%s): %s", x.Name, x.Model, fmt.Sprintf(w["smartWarnings"], x.Warnings)))
		}
		if x.TempC > hottest {
			hottest = x.TempC
		}
		if x.TempC > 55 {
			disk.Status = worst(disk.Status, Warning)
			disk.Lines = append(disk.Lines, fmt.Sprintf("%s: %d °C", x.Name, x.TempC))
		}
	}
	summary := fmt.Sprintf(w["diskSummary"], len(d.Disks), len(d.Disks)-unhealthy)
	if hottest > 0 {
		summary += fmt.Sprintf(" · %s %d °C", w["hottest"], hottest)
	}
	disk.Lines = append([]string{summary}, disk.Lines...)
	add(disk)

	backups := Section{Title: w["backups"], Status: OK}
	enabled := 0
	for _, b := range d.Backups {
		if !b.Enabled {
			continue
		}
		enabled++
		line := b.Name + ": "
		switch {
		case b.Overdue:
			backups.Status = worst(backups.Status, Error)
			line += w["overdue"]
		case b.LastError != "":
			backups.Status = worst(backups.Status, Warning)
			line += w["lastFailed"]
		case b.LastOK == nil:
			line += w["neverRan"]
		default:
			line += fmt.Sprintf(w["lastOK"], b.LastOK.In(now.Location()).Format("02.01. 15:04"))
		}
		backups.Lines = append(backups.Lines, line)
	}
	if enabled == 0 {
		backups.Status = Warning
		backups.Lines = append(backups.Lines, w["noBackups"])
	}
	add(backups)

	system := Section{Title: w["system"], Status: OK}
	system.Lines = append(system.Lines, fmt.Sprintf("TitanOS %s · %s %s", d.Version, w["uptime"], duration(w, d.Uptime)))
	if d.MemoryPercent >= 0 {
		system.Lines = append(system.Lines, fmt.Sprintf("%s %d %%", w["memory"], d.MemoryPercent))
	}
	if d.CPUWatts != nil {
		system.Lines = append(system.Lines, fmt.Sprintf("%s %.0f W", w["cpuPower"], *d.CPUWatts))
	}
	if d.UpdateAvailable != "" {
		system.Status = Warning
		system.Lines = append(system.Lines, fmt.Sprintf(w["update"], d.UpdateAvailable))
	}
	if d.UPS != nil {
		switch {
		case !d.UPS.Connected:
			system.Status = worst(system.Status, Warning)
			system.Lines = append(system.Lines, w["upsLost"])
		case d.UPS.OnBattery:
			system.Status = worst(system.Status, Warning)
			system.Lines = append(system.Lines, w["upsBattery"])
		default:
			line := w["upsOK"]
			if d.UPS.Charge >= 0 {
				line += fmt.Sprintf(" (%d %%)", d.UPS.Charge)
			}
			system.Lines = append(system.Lines, line)
		}
	}
	add(system)

	messages := Section{Title: w["messages"], Status: OK}
	switch {
	case d.Errors > 0:
		messages.Status = Warning
	case d.Warnings > 0:
		messages.Status = Warning
	}
	messages.Lines = append(messages.Lines, fmt.Sprintf(w["messageCount"], d.Errors, d.Warnings))
	add(messages)

	r.Title = fmt.Sprintf(w["title"], w["status."+r.Status])
	return r
}

func stateWord(w map[string]string, v Volume) string {
	if v.Locked {
		return w["locked"]
	}
	text, ok := w["state."+v.State]
	if !ok {
		text = v.State
	}
	if !v.Mounted {
		return text + ", " + w["notMounted"]
	}
	return text
}

func bytes(n uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	v, i := float64(n), 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	if i == 0 || v >= 100 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func duration(w map[string]string, d time.Duration) string {
	days := int(d.Hours()) / 24
	if days > 0 {
		return fmt.Sprintf(w["days"], days)
	}
	return fmt.Sprintf(w["hours"], int(d.Hours()))
}

// Manager keeps the settings and sends on schedule.
type Manager struct {
	store *config.Store
	// Collect gathers the data; Send delivers title and text.
	Collect  func() Data
	Send     func(title, text, status string) error
	Language func() string
	// Timezone names the time zone of the NAS (empty: local).
	Timezone func() string
	// sentFile remembers the last report over a restart of titand (RAM).
	sentFile string
	now      func() time.Time

	mu       sync.Mutex
	loaded   bool
	settings Settings
	lastSent time.Time
	failedAt time.Time
}

// New returns a manager for the configuration in store.
func New(store *config.Store) *Manager {
	return &Manager{store: store, sentFile: "/run/titanos/health-report-sent", now: time.Now,
		Language: func() string { return "en" }, Timezone: func() string { return "" }}
}

// Load reads the settings from /config.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := defaults()
	if err := m.store.ReadJSON(settingsFile, &s); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if s.check() != nil {
		enabled := s.Enabled
		s = defaults()
		s.Enabled = enabled
	}
	m.settings, m.loaded = s, true
	return nil
}

// Settings returns the current settings.
func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

// Set stores new settings.
func (m *Manager) Set(s Settings) error {
	if err := s.check(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return errors.New("health report: not loaded")
	}
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		return err
	}
	m.settings = s
	return nil
}

func (m *Manager) location() *time.Location {
	if name := m.Timezone(); name != "" {
		if zone, err := time.LoadLocation(name); err == nil {
			return zone
		}
	}
	return time.Local
}

// Preview makes a report now without sending it.
func (m *Manager) Preview() Report {
	now := m.now().In(m.location())
	return Build(m.Collect(), m.Language(), now)
}

// SendNow makes and sends a report.
func (m *Manager) SendNow() (Report, error) {
	report := m.Preview()
	if err := m.Send(report.Title, report.Text(), report.Status); err != nil {
		return report, err
	}
	m.mu.Lock()
	m.lastSent = report.Generated
	m.mu.Unlock()
	return report, nil
}

// lastDue is the latest scheduled time at or before now.
func lastDue(s Settings, now time.Time) time.Time {
	var hour, minute int
	_, _ = fmt.Sscanf(s.Time, "%d:%d", &hour, &minute) // a value that cannot be read leaves the zero value, which the code handles
	day := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if s.Every == "monthly" {
		due := time.Date(now.Year(), now.Month(), s.Day, hour, minute, 0, 0, now.Location())
		if due.After(now) {
			due = due.AddDate(0, -1, 0)
		}
		return due
	}
	back := (int(now.Weekday()) - s.Weekday + 7) % 7
	due := day.AddDate(0, 0, -back)
	if due.After(now) {
		due = due.AddDate(0, 0, -7)
	}
	return due
}

// Next is when the next report goes out; zero while off.
func (m *Manager) Next() time.Time {
	m.mu.Lock()
	s := m.settings
	m.mu.Unlock()
	if !s.Enabled {
		return time.Time{}
	}
	now := m.now().In(m.location())
	due := lastDue(s, now)
	if s.Every == "monthly" {
		return due.AddDate(0, 1, 0)
	}
	return due.AddDate(0, 0, 7)
}

// Run sends the reports on schedule until ctx ends. A report missed while
// the NAS was off goes out after the start if its time is less than a day
// ago; the time of the last one is kept in /run over titand restarts.
func (m *Manager) Run(ctx context.Context) {
	if data, err := readSent(m.sentFile); err == nil {
		m.mu.Lock()
		m.lastSent = data
		m.mu.Unlock()
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		m.tick()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) tick() {
	m.mu.Lock()
	s, loaded, last, failed := m.settings, m.loaded, m.lastSent, m.failedAt
	m.mu.Unlock()
	if !loaded || !s.Enabled || m.Collect == nil || m.Send == nil {
		return
	}
	now := m.now().In(m.location())
	due := lastDue(s, now)
	if !last.Before(due) || now.Sub(due) > 24*time.Hour || now.Sub(failed) < time.Hour {
		return
	}
	report, err := m.SendNow()
	if err != nil {
		// Again in an hour, not every minute.
		slog.Warn("health report not sent", "err", err)
		m.mu.Lock()
		m.failedAt = now
		m.mu.Unlock()
		return
	}
	writeSent(m.sentFile, report.Generated)
}

func readSent(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
}

func writeSent(path string, at time.Time) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		_ = os.WriteFile(path, []byte(at.UTC().Format(time.RFC3339)+"\n"), 0o644) // a runtime or state file only; without it the default applies
	}
}
