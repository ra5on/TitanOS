package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"
)

// ErrNoDisk means a disk asked for by name does not exist (any more).
var ErrNoDisk = errors.New("storage: no such disk")

// SMART health of a disk in the overview. The badge thresholds are listed in
// docs/SMART.md; change both together.
const (
	SmartOK          = "ok"
	SmartWarning     = "warning"
	SmartFailed      = "failed"
	SmartUnavailable = "unavailable" // the disk reports no SMART (virtual disk, USB bridge)
	SmartUnknown     = "unknown"     // not read yet: asleep since titand started
)

// Warning thresholds. Counters of damaged sectors and cable errors never go
// back to zero on their own, so any value above zero is worth a look.
const (
	smartHotDiskC   = 55 // HDDs and SATA SSDs, the same as the notification
	smartHotNVMeC   = 70
	smartWornNVMe   = 90 // percentage_used of an NVMe SSD
	smartRefreshAge = 30 * time.Second
)

// Warning codes of a SmartReport, translated by the UI.
const (
	SmartWarnFailed        = "smartFailed"   // the disk itself says it is failing
	SmartWarnAttribute     = "attributeNow"  // an ATA attribute is below its threshold now
	SmartWarnAttributePast = "attributePast" // … was below it in the past
	SmartWarnReallocated   = "reallocated"
	SmartWarnPending       = "pending"
	SmartWarnUncorrectable = "uncorrectable"
	SmartWarnCRC           = "crc"
	SmartWarnTemperature   = "temperature"
	SmartWarnNVMeCritical  = "nvmeCritical"
	SmartWarnWear          = "wear"
	SmartWarnMediaErrors   = "mediaErrors"
	SmartWarnSpare         = "spare"
)

// SmartReport is the SMART state of one disk.
type SmartReport struct {
	Disk   string `json:"disk"` // sda
	Path   string `json:"path"`
	Model  string `json:"model"`
	Serial string `json:"serial"`
	Kind   string `json:"kind"`
	Role   string `json:"role"`
	Volume string `json:"volume,omitempty"`
	// State is judged from Values: when the disk sleeps, from the last values
	// read while it was awake.
	State    string   `json:"state"`
	Warnings []string `json:"warnings"`
	// Sleeping is set when the disk was in standby at the last look and was
	// left asleep. Values (if any) are then from ReadAt.
	Sleeping  bool         `json:"sleeping"`
	CheckedAt *time.Time   `json:"checkedAt,omitempty"`
	ReadAt    *time.Time   `json:"readAt,omitempty"`
	Values    *SmartValues `json:"values"`
}

// SmartValues are the parsed values of `smartctl --json`. A pointer is nil
// when the disk does not report that value.
type SmartValues struct {
	Protocol     string `json:"protocol"` // ATA, NVMe, SCSI
	ModelFamily  string `json:"modelFamily,omitempty"`
	Firmware     string `json:"firmware,omitempty"`
	Passed       bool   `json:"passed"`
	TemperatureC *int   `json:"temperatureC"`
	PowerOnHours *int64 `json:"powerOnHours"`
	PowerCycles  *int64 `json:"powerCycles"`
	RotationRate int    `json:"rotationRate"`
	// ATA attributes 5, 197, 198 (or 187 when larger) and 199; for SAS the
	// grown defect list and the uncorrected errors of the error counter log.
	ReallocatedSectors   *uint64          `json:"reallocatedSectors"`
	PendingSectors       *uint64          `json:"pendingSectors"`
	UncorrectableSectors *uint64          `json:"uncorrectableSectors"`
	CRCErrors            *uint64          `json:"crcErrors"`
	NVMe                 *SmartNVMe       `json:"nvme,omitempty"`
	Attributes           []SmartAttribute `json:"attributes,omitempty"`
}

// SmartNVMe is the health information log of an NVMe SSD.
type SmartNVMe struct {
	CriticalWarning         int    `json:"criticalWarning"`
	PercentageUsed          int    `json:"percentageUsed"`
	AvailableSpare          int    `json:"availableSpare"`
	AvailableSpareThreshold int    `json:"availableSpareThreshold"`
	MediaErrors             uint64 `json:"mediaErrors"`
	ErrorLogEntries         uint64 `json:"errorLogEntries"`
	UnsafeShutdowns         uint64 `json:"unsafeShutdowns"`
	DataUnitsRead           uint64 `json:"dataUnitsRead"`
	DataUnitsWritten        uint64 `json:"dataUnitsWritten"`
}

// SmartAttribute is one row of the attribute table. For NVMe the table holds
// the fields of the health log, with only Name and Raw set.
type SmartAttribute struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Value      int    `json:"value"`
	Worst      int    `json:"worst"`
	Threshold  int    `json:"threshold"`
	Raw        string `json:"raw"`
	Flags      string `json:"flags,omitempty"`
	Prefailure bool   `json:"prefailure"`
	WhenFailed string `json:"whenFailed,omitempty"` // "now", "past" or ""
	// Warn marks the row that caused a warning.
	Warn bool `json:"warn,omitempty"`
}

// SmartDump is the raw smartctl answer of one disk for the diagnostic bundle.
type SmartDump struct {
	Disk      string          `json:"disk"`
	State     string          `json:"state"`
	Sleeping  bool            `json:"sleeping"`
	CheckedAt *time.Time      `json:"checkedAt,omitempty"`
	ReadAt    *time.Time      `json:"readAt,omitempty"`
	Smartctl  json.RawMessage `json:"smartctl,omitempty"`
}

// smartEntry is what RefreshSmart remembers of a disk, in RAM only.
type smartEntry struct {
	checked  time.Time
	read     time.Time // zero: never read awake
	sleeping bool
	values   *SmartValues
	raw      string // the smartctl answer the values come from
}

// smartSleeping reports whether smartctl skipped the disk because of
// --nocheck=standby: exit bit 1 and the message naming the power mode.
func smartSleeping(output string) bool {
	var data struct {
		Smartctl struct {
			ExitStatus int `json:"exit_status"`
			Messages   []struct {
				String string `json:"string"`
			} `json:"messages"`
		} `json:"smartctl"`
	}
	if json.Unmarshal([]byte(output), &data) != nil || data.Smartctl.ExitStatus&2 == 0 {
		return false
	}
	for _, message := range data.Smartctl.Messages {
		text := strings.ToUpper(message.String)
		if strings.Contains(text, "STANDBY") || strings.Contains(text, "SLEEP") {
			return true
		}
	}
	return false
}

type smartctlAttribute struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Value      int    `json:"value"`
	Worst      int    `json:"worst"`
	Thresh     int    `json:"thresh"`
	WhenFailed string `json:"when_failed"`
	Flags      struct {
		String     string `json:"string"`
		Prefailure bool   `json:"prefailure"`
	} `json:"flags"`
	Raw struct {
		Value  uint64 `json:"value"`
		String string `json:"string"`
	} `json:"raw"`
}

// nvmeFields is the order of the NVMe health log in the attribute table.
var nvmeFields = []string{
	"critical_warning", "temperature", "available_spare", "available_spare_threshold", "percentage_used",
	"data_units_read", "data_units_written", "host_reads", "host_writes", "controller_busy_time",
	"power_cycles", "power_on_hours", "unsafe_shutdowns", "media_errors", "num_err_log_entries",
	"warning_temp_time", "critical_comp_time", "temperature_sensors",
}

// parseSmartValues reads `smartctl --json` in full; nil if the disk reports
// no SMART at all.
func parseSmartValues(output string) *SmartValues {
	var data struct {
		Device struct {
			Protocol string `json:"protocol"`
		} `json:"device"`
		ModelFamily string `json:"model_family"`
		Firmware    string `json:"firmware_version"`
		SmartStatus *struct {
			Passed bool `json:"passed"`
		} `json:"smart_status"`
		Temperature *struct {
			Current *int `json:"current"`
		} `json:"temperature"`
		PowerOnTime *struct {
			Hours int64 `json:"hours"`
		} `json:"power_on_time"`
		PowerCycles  *int64 `json:"power_cycle_count"`
		RotationRate int    `json:"rotation_rate"`
		ATA          *struct {
			Table []smartctlAttribute `json:"table"`
		} `json:"ata_smart_attributes"`
		NVMe          map[string]json.RawMessage `json:"nvme_smart_health_information_log"`
		GrownDefects  *uint64                    `json:"scsi_grown_defect_list"`
		SCSIErrorLogs *struct {
			Read, Write, Verify *struct {
				Uncorrected uint64 `json:"total_uncorrected_errors"`
			}
		} `json:"scsi_error_counter_log"`
	}
	if json.Unmarshal([]byte(output), &data) != nil || data.SmartStatus == nil {
		return nil
	}
	values := &SmartValues{
		Protocol: data.Device.Protocol, ModelFamily: data.ModelFamily, Firmware: data.Firmware,
		Passed: data.SmartStatus.Passed, PowerCycles: data.PowerCycles, RotationRate: data.RotationRate,
	}
	if data.Temperature != nil && data.Temperature.Current != nil {
		values.TemperatureC = data.Temperature.Current
	}
	if data.PowerOnTime != nil {
		values.PowerOnHours = &data.PowerOnTime.Hours
	}
	if data.ATA != nil {
		var reported *uint64
		for _, a := range data.ATA.Table {
			raw := a.Raw.Value
			switch a.ID {
			case 5:
				values.ReallocatedSectors = &raw
			case 197:
				values.PendingSectors = &raw
			case 198:
				if values.UncorrectableSectors == nil || raw > *values.UncorrectableSectors {
					values.UncorrectableSectors = &raw
				}
			case 187:
				reported = &raw
			case 199:
				values.CRCErrors = &raw
			}
			text := a.Raw.String
			if text == "" {
				text = jsonNumber(raw)
			}
			values.Attributes = append(values.Attributes, SmartAttribute{
				ID: a.ID, Name: a.Name, Value: a.Value, Worst: a.Worst, Threshold: a.Thresh, Raw: text,
				Flags: strings.TrimSpace(a.Flags.String), Prefailure: a.Flags.Prefailure, WhenFailed: strings.ToLower(a.WhenFailed),
			})
		}
		// 187 (reported uncorrectable) stands in for 198 on drives without it.
		if reported != nil && (values.UncorrectableSectors == nil || *reported > *values.UncorrectableSectors) {
			values.UncorrectableSectors = reported
		}
	}
	if data.NVMe != nil {
		nvme := &SmartNVMe{}
		number := func(key string) uint64 {
			var n uint64
			json.Unmarshal(data.NVMe[key], &n)
			return n
		}
		nvme.CriticalWarning = int(number("critical_warning"))
		nvme.PercentageUsed = int(number("percentage_used"))
		nvme.AvailableSpare = int(number("available_spare"))
		nvme.AvailableSpareThreshold = int(number("available_spare_threshold"))
		nvme.MediaErrors = number("media_errors")
		nvme.ErrorLogEntries = number("num_err_log_entries")
		nvme.UnsafeShutdowns = number("unsafe_shutdowns")
		nvme.DataUnitsRead = number("data_units_read")
		nvme.DataUnitsWritten = number("data_units_written")
		values.NVMe = nvme
		if values.PowerCycles == nil {
			if _, ok := data.NVMe["power_cycles"]; ok {
				cycles := int64(number("power_cycles"))
				values.PowerCycles = &cycles
			}
		}
		keys := make([]string, 0, len(data.NVMe))
		for key := range data.NVMe {
			if !slices.Contains(nvmeFields, key) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range append(slices.Clone(nvmeFields), keys...) {
			raw, ok := data.NVMe[key]
			if !ok {
				continue
			}
			values.Attributes = append(values.Attributes, SmartAttribute{Name: key, Raw: compactJSON(raw)})
		}
	}
	if data.GrownDefects != nil {
		values.ReallocatedSectors = data.GrownDefects
	}
	if logs := data.SCSIErrorLogs; logs != nil {
		var total uint64
		for _, part := range []*struct {
			Uncorrected uint64 `json:"total_uncorrected_errors"`
		}{logs.Read, logs.Write, logs.Verify} {
			if part != nil {
				total += part.Uncorrected
			}
		}
		values.UncorrectableSectors = &total
	}
	return values
}

func jsonNumber(n uint64) string {
	data, _ := json.Marshal(n)
	return string(data)
}

func compactJSON(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return strings.TrimSpace(string(raw))
	}
	return b.String()
}

// judgeSmart decides the badge of a disk from its values, by the thresholds
// documented in docs/SMART.md. It marks the attributes that caused a warning.
func judgeSmart(values *SmartValues, kind string) (string, []string) {
	warnings := []string{}
	if values == nil {
		return SmartUnavailable, warnings
	}
	failed := false
	add := func(code string) {
		if !slices.Contains(warnings, code) {
			warnings = append(warnings, code)
		}
	}
	if !values.Passed {
		failed = true
		add(SmartWarnFailed)
	}
	positive := func(value *uint64) bool { return value != nil && *value > 0 }
	if positive(values.ReallocatedSectors) {
		add(SmartWarnReallocated)
	}
	if positive(values.PendingSectors) {
		add(SmartWarnPending)
	}
	if positive(values.UncorrectableSectors) {
		add(SmartWarnUncorrectable)
	}
	if positive(values.CRCErrors) {
		add(SmartWarnCRC)
	}
	hot := smartHotDiskC
	if kind == "nvme" || values.NVMe != nil {
		hot = smartHotNVMeC
	}
	if values.TemperatureC != nil && *values.TemperatureC >= hot {
		add(SmartWarnTemperature)
	}
	if nvme := values.NVMe; nvme != nil {
		if nvme.CriticalWarning != 0 {
			add(SmartWarnNVMeCritical)
			// Reliability degraded, read-only or volatile backup failed.
			if nvme.CriticalWarning&0x1c != 0 {
				failed = true
			}
		}
		if nvme.PercentageUsed >= smartWornNVMe {
			add(SmartWarnWear)
		}
		if nvme.MediaErrors > 0 {
			add(SmartWarnMediaErrors)
		}
		if nvme.AvailableSpareThreshold > 0 && nvme.AvailableSpare <= nvme.AvailableSpareThreshold {
			add(SmartWarnSpare)
		}
	}
	for i := range values.Attributes {
		a := &values.Attributes[i]
		switch a.WhenFailed {
		case "now":
			failed = true
			a.Warn = true
			add(SmartWarnAttribute)
		case "past":
			a.Warn = true
			add(SmartWarnAttributePast)
		}
		if values.NVMe == nil && slices.Contains([]int{5, 187, 197, 198, 199}, a.ID) && a.Raw != "0" && a.Raw != "" {
			a.Warn = true
		}
	}
	switch {
	case failed:
		return SmartFailed, warnings
	case len(warnings) > 0:
		return SmartWarning, warnings
	}
	return SmartOK, warnings
}

// refreshSmartIfOlder reads SMART again unless the last read is younger
// than maxAge. Concurrent callers wait for one read and share it.
func (m *Manager) refreshSmartIfOlder(ctx context.Context, maxAge time.Duration) {
	m.smartRefresh.Lock()
	defer m.smartRefresh.Unlock()
	m.mu.RLock()
	fresh := !m.smartTime.IsZero() && m.now().Sub(m.smartTime) < maxAge
	m.mu.RUnlock()
	if !fresh {
		m.refreshSmart(ctx)
	}
}

// RefreshSmartSoon reads SMART again unless that happened in the last 30
// seconds. Sleeping disks stay asleep either way.
func (m *Manager) RefreshSmartSoon(ctx context.Context) {
	m.refreshSmartIfOlder(ctx, smartRefreshAge)
}

// SmartReports returns the SMART state of every disk from the RAM cache.
// It runs no smartctl; withAttributes adds the full attribute tables.
func (m *Manager) SmartReports(ctx context.Context, withAttributes bool) ([]SmartReport, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	reports := make([]SmartReport, 0, len(status.Disks))
	for _, disk := range status.Disks {
		reports = append(reports, m.smartReport(disk, withAttributes))
	}
	return reports, nil
}

// SmartReport returns the SMART state of one disk with its attribute table.
func (m *Manager) SmartReport(ctx context.Context, name string) (SmartReport, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return SmartReport{}, err
	}
	for _, disk := range status.Disks {
		if disk.Name == name {
			m.mu.RLock()
			defer m.mu.RUnlock()
			return m.smartReport(disk, true), nil
		}
	}
	return SmartReport{}, ErrNoDisk
}

// smartReport builds the report of a disk; m.mu is held for reading.
func (m *Manager) smartReport(disk Disk, withAttributes bool) SmartReport {
	report := SmartReport{
		Disk: disk.Name, Path: disk.Path, Model: disk.Model, Serial: disk.Serial, Kind: disk.Kind,
		Role: disk.Role, Volume: disk.Volume, State: SmartUnknown, Warnings: []string{},
	}
	entry := m.smartDisks[disk.Path]
	if entry == nil {
		return report
	}
	checked := entry.checked
	report.CheckedAt, report.Sleeping = &checked, entry.sleeping
	if !entry.read.IsZero() {
		read := entry.read
		report.ReadAt = &read
	}
	if entry.values == nil {
		if !entry.sleeping {
			report.State = SmartUnavailable
		}
		return report
	}
	// A copy, so that judging and trimming leave the cache alone.
	values := *entry.values
	values.Attributes = slices.Clone(entry.values.Attributes)
	report.State, report.Warnings = judgeSmart(&values, disk.Kind)
	if !withAttributes {
		values.Attributes = nil
	}
	report.Values = &values
	return report
}

// SmartDumps returns the raw smartctl answers for the diagnostic bundle,
// after reading the awake disks again (sleeping disks are not woken).
// Serial numbers and world wide names are taken out.
func (m *Manager) SmartDumps(ctx context.Context) []SmartDump {
	m.RefreshSmartSoon(ctx)
	reports, err := m.SmartReports(ctx, false)
	if err != nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	dumps := make([]SmartDump, 0, len(reports))
	for _, report := range reports {
		dump := SmartDump{Disk: report.Disk, State: report.State, Sleeping: report.Sleeping, CheckedAt: report.CheckedAt, ReadAt: report.ReadAt}
		if entry := m.smartDisks[report.Path]; entry != nil && entry.raw != "" {
			dump.Smartctl = withoutIdentity(entry.raw)
		}
		dumps = append(dumps, dump)
	}
	return dumps
}

// identityKeys are the fields of smartctl's JSON that identify one device.
var identityKeys = []string{"serial_number", "wwn", "logical_unit_id", "eui64", "nguid", "local_time"}

// withoutIdentity drops the identifying fields; nil if output is no JSON.
func withoutIdentity(output string) json.RawMessage {
	var data any
	if json.Unmarshal([]byte(output), &data) != nil {
		return nil
	}
	var strip func(any)
	strip = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for _, key := range identityKeys {
				delete(v, key)
			}
			for _, child := range v {
				strip(child)
			}
		case []any:
			for _, child := range v {
				strip(child)
			}
		}
	}
	strip(data)
	clean, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	return clean
}

// WearWarnings are the SMART warnings that tell of wear or damage before a
// disk fails: worth a notification while the data can still be copied.
var WearWarnings = []string{SmartWarnReallocated, SmartWarnPending, SmartWarnUncorrectable, SmartWarnCRC,
	SmartWarnAttribute, SmartWarnNVMeCritical, SmartWarnWear, SmartWarnMediaErrors, SmartWarnSpare}

// smartDefects adds up reallocated, pending and uncorrectable sectors, CRC
// errors and NVMe media errors: counters that only grow.
func smartDefects(values *SmartValues) uint64 {
	var total uint64
	for _, value := range []*uint64{values.ReallocatedSectors, values.PendingSectors, values.UncorrectableSectors, values.CRCErrors} {
		if value != nil {
			total += *value
		}
	}
	if values.NVMe != nil {
		total += values.NVMe.MediaErrors
	}
	return total
}
