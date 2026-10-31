package api

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/diagnose"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// The reading tools of the catalog for owners and administrators. Each
// returns a compact JSON object for the model.

func (s *Server) toolSystemStatus(ctx context.Context) (any, error) {
	info := s.opts.Monitor.Info()
	type temperature struct {
		Sensor    string   `json:"sensor"`
		Celsius   float64  `json:"celsius"`
		CriticalC *float64 `json:"criticalC,omitempty"`
	}
	temperatures := []temperature{}
	for _, t := range info.Temperatures {
		if t.Celsius == nil {
			continue
		}
		name := strings.TrimSpace(strings.Join([]string{t.Chip, t.Label, t.Device}, " "))
		temperatures = append(temperatures, temperature{Sensor: name, Celsius: *t.Celsius, CriticalC: t.CriticalC})
	}
	res := map[string]any{
		"hostname":      info.Hostname,
		"version":       s.opts.Version,
		"kernel":        info.Kernel,
		"uptimeSeconds": info.UptimeSeconds,
		"cpu": map[string]any{
			"model": info.CPU.Model, "threads": info.CPU.Threads, "usagePercent": math.Round(info.CPU.UsagePercent*10) / 10,
		},
		"memory": map[string]uint64{
			"totalBytes": info.Memory.TotalBytes, "usedBytes": info.Memory.UsedBytes, "availableBytes": info.Memory.AvailableBytes,
		},
		"temperatures": temperatures,
		"stickPresent": info.Stick.Present,
	}
	if s.opts.Update != nil && s.opts.Update.Base != "" {
		res["baseVersion"] = s.opts.Update.Base
	}
	if info.Image != nil {
		res["slot"] = info.Image.Slot
		res["overlay"] = map[string]uint64{"usedBytes": info.Image.OverlayUsedBytes, "totalBytes": info.Image.OverlayTotalBytes}
	}
	return res, nil
}

func (s *Server) toolNotifications(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Limit       int    `json:"limit"`
		MinSeverity string `json:"minSeverity"`
	}
	if err := toolcatalog.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	rank := map[string]int{notify.Info: 0, notify.Warning: 1, notify.Error: 2}
	least, known := rank[req.MinSeverity]
	if req.MinSeverity != "" && !known {
		return nil, toolcatalog.Errorf("minSeverity must be info, warning or error")
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	type message struct {
		Time     time.Time `json:"time"`
		Severity string    `json:"severity"`
		Kind     string    `json:"kind"`
		Subject  string    `json:"subject"`
		Title    string    `json:"title"`
		Text     string    `json:"text"`
	}
	list := []message{}
	for _, m := range s.opts.Notify.Messages() {
		if rank[m.Severity] < least {
			continue
		}
		list = append(list, message{Time: m.Time, Severity: m.Severity, Kind: m.Kind, Subject: m.Subject, Title: m.Title, Text: m.Text})
		if len(list) == min(req.Limit, 100) {
			break
		}
	}
	return map[string]any{"notifications": list}, nil
}

func (s *Server) toolUpdateStatus(ctx context.Context) (any, error) {
	if s.opts.Update == nil {
		return map[string]any{"version": s.opts.Version}, nil
	}
	status := s.opts.Update.Status()
	res := map[string]any{
		"version":     status.Version,
		"channel":     status.Settings.Channel,
		"autoInstall": status.Settings.AutoInstall,
		"slots":       status.Slots,
		"unconfirmed": status.Unconfirmed,
		"lastCheck":   status.LastCheck,
		"nextCheck":   status.NextCheck,
		"checkEvery":  status.Settings.CheckEvery,
	}
	if status.Base != "" {
		res["baseVersion"] = status.Base
	}
	if status.CheckError != "" {
		res["checkError"] = status.CheckError
	}
	if status.Pending != nil {
		res["pendingRestart"] = map[string]any{"kind": status.Pending.Kind, "version": status.Pending.Version, "since": status.Pending.Since}
	}
	if a := status.Available; a != nil {
		changelog := a.Changelog
		if len(changelog) > 4000 {
			changelog = changelog[:4000] + " …"
		}
		res["available"] = map[string]any{"version": a.Version, "channel": a.Channel, "released": a.Released, "kind": status.Kind, "changelog": changelog}
	}
	return res, nil
}

func (s *Server) toolUPSStatus(ctx context.Context) (any, error) {
	if s.opts.UPS == nil {
		return map[string]any{"mode": "none"}, nil
	}
	status := s.opts.UPS.Status()
	// Raw is everything the driver said; the fields below are what counts.
	status.Raw = ""
	return status, nil
}

func (s *Server) toolStorage(ctx context.Context) (any, error) {
	status, err := s.opts.Storage.Status(ctx)
	if err != nil {
		return nil, err
	}
	type volume struct {
		Name       string `json:"name"`
		Level      string `json:"level"`
		Filesystem string `json:"filesystem"`
		State      string `json:"state"`
		Mounted    bool   `json:"mounted"`
		SizeBytes  uint64 `json:"sizeBytes"`
		UsedBytes  uint64 `json:"usedBytes"`
		RaidDisks  int    `json:"raidDisks"`
		Members    any    `json:"members"`
		Sync       any    `json:"sync,omitempty"`
		Mismatches uint64 `json:"mismatches,omitempty"`
	}
	type disk struct {
		Name        string `json:"name"`
		Model       string `json:"model"`
		Kind        string `json:"kind"`
		SizeBytes   uint64 `json:"sizeBytes"`
		Role        string `json:"role"`
		Volume      string `json:"volume,omitempty"`
		MemberState string `json:"memberState,omitempty"`
		Testing     bool   `json:"testing,omitempty"`
		Smart       any    `json:"smart"`
	}
	volumes := []volume{}
	for _, v := range status.Volumes {
		item := volume{Name: v.Name, Level: v.Level, Filesystem: v.Filesystem, State: v.State, Mounted: v.Mounted,
			SizeBytes: v.SizeBytes, UsedBytes: v.UsedBytes, RaidDisks: v.RaidDisks, Members: v.Members, Mismatches: v.Mismatches}
		if v.Sync != nil {
			item.Sync = v.Sync
		}
		volumes = append(volumes, item)
	}
	disks := []disk{}
	for _, d := range status.Disks {
		disks = append(disks, disk{Name: d.Name, Model: d.Model, Kind: d.Kind, SizeBytes: d.SizeBytes, Role: d.Role,
			Volume: d.Volume, MemberState: d.MemberState, Testing: d.Testing, Smart: d.Smart})
	}
	return map[string]any{"volumes": volumes, "disks": disks}, nil
}

func (s *Server) toolShares(ctx context.Context) (any, error) {
	mounted := s.opts.Storage.Mounted()
	type share struct {
		Root        string `json:"root"`
		Writable    bool   `json:"writable"`
		Name        string `json:"name"`
		Volume      string `json:"volume"`
		Path        string `json:"path"`
		Available   bool   `json:"available"`
		SMB         bool   `json:"smb"`
		NFS         bool   `json:"nfs"`
		Guest       bool   `json:"guest"`
		Recycle     bool   `json:"recycleBin"`
		TimeMachine bool   `json:"timeMachine"`
	}
	// Who may open a share is left out: it names the accounts.
	list := []share{}
	for _, sh := range s.opts.Shares.List() {
		list = append(list, share{Root: "share/" + sh.Name, Writable: slices.Contains(mounted, sh.Volume), Name: sh.Name, Volume: sh.Volume, Path: s.opts.Shares.Path(sh), Available: slices.Contains(mounted, sh.Volume),
			SMB: sh.SMB, NFS: sh.NFS.Enabled, Guest: sh.Guest, Recycle: sh.Recycle.Enabled, TimeMachine: sh.TimeMachine.Enabled})
	}
	return map[string]any{"shares": list}, nil
}

func (s *Server) toolDocker(ctx context.Context) (any, error) {
	status := s.opts.Docker.Status(ctx)
	type container struct {
		ID              string    `json:"id"`
		Name            string    `json:"name"`
		Image           string    `json:"image"`
		State           string    `json:"state"`
		Since           time.Time `json:"since"`
		App             string    `json:"app,omitempty"`
		CPUPercent      float64   `json:"cpuPercent"`
		MemoryBytes     uint64    `json:"memoryBytes"`
		Ports           any       `json:"ports"`
		UpdateAvailable bool      `json:"updateAvailable"`
		UpdateState     string    `json:"updateState"`
		UpdateError     string    `json:"updateError,omitempty"`
	}
	list := []container{}
	for _, c := range status.Containers {
		list = append(list, container{ID: c.ID, Name: c.Name, Image: c.Image, State: c.State, Since: c.Since, App: c.App,
			CPUPercent: math.Round(c.CPUPercent*10) / 10, MemoryBytes: c.MemoryBytes, Ports: c.Ports, UpdateAvailable: c.UpdateAvailable, UpdateState: c.UpdateState, UpdateError: c.UpdateError})
	}
	failed := []string{}
	for name, job := range status.Jobs {
		if job.State == "failed" {
			failed = append(failed, name)
		}
	}
	slices.Sort(failed)
	res := map[string]any{"observedAt": time.Now(), "updateConclusion": "Only current/available entries were checked successfully; failed/not_checked/pinned are not evidence of no updates.", "engineRunning": status.Running, "enabled": status.Settings.Enabled, "containers": list, "failedAppSetups": failed}
	if !status.UpdatesChecked.IsZero() {
		res["updatesChecked"] = status.UpdatesChecked
	}
	return res, nil
}

// redactor hides addresses, account names and secrets in log lines.
func (s *Server) redactor() *diagnose.Redactor {
	var accounts []string
	for _, user := range s.opts.Users.List() {
		accounts = append(accounts, user.Name)
	}
	return diagnose.NewRedactor("", accounts)
}

func (s *Server) toolContainerLogs(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Container string `json:"container"`
		Lines     int    `json:"lines"`
	}
	if err := toolcatalog.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	name := strings.TrimPrefix(strings.TrimSpace(req.Container), "/")
	var id string
	for _, c := range s.opts.Docker.Status(ctx).Containers {
		if c.Name == name || len(name) >= 12 && strings.HasPrefix(c.ID, name) {
			id = c.ID
			break
		}
	}
	if id == "" {
		return nil, toolcatalog.Errorf("no container %q; docker_list shows the names", req.Container)
	}
	logs, err := s.opts.Docker.Logs(ctx, id, clampLines(req.Lines))
	if err != nil {
		return nil, err
	}
	return map[string]any{"container": name, "logs": s.redactor().Text(logs)}, nil
}

func (s *Server) toolVMs(ctx context.Context) (any, error) {
	type machine struct {
		Name        string    `json:"name"`
		DisplayName string    `json:"displayName,omitempty"`
		Description string    `json:"description,omitempty"`
		State       string    `json:"state"`
		Available   bool      `json:"available"`
		VCPUs       int       `json:"vcpus"`
		MemoryMiB   int       `json:"memoryMiB"`
		CPUPercent  float64   `json:"cpuPercent"`
		MemoryBytes uint64    `json:"memoryBytes"`
		Since       time.Time `json:"since"`
		Autostart   bool      `json:"autostart"`
		Addresses   []string  `json:"addresses,omitempty"`
	}
	list := []machine{}
	for _, vm := range s.opts.VMs.List(s.opts.Storage.Mounted()) {
		item := machine{Name: vm.Name, DisplayName: vm.DisplayName, Description: vm.Description, State: vm.State, Available: vm.Available,
			VCPUs: vm.VCPUs, MemoryMiB: vm.MemoryMiB, CPUPercent: math.Round(vm.CPUPercent*10) / 10, MemoryBytes: vm.MemoryBytes, Since: vm.Since, Autostart: vm.Autostart}
		if vm.State == "running" {
			lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
			if addresses, err := s.opts.VMs.Addresses(lookup, vm.Name); err == nil {
				item.Addresses = addresses.Addresses
			}
			cancel()
		}
		list = append(list, item)
	}
	slices.SortFunc(list, func(a, b machine) int { return strings.Compare(a.Name, b.Name) })
	return map[string]any{"kvm": s.opts.VMs.HasKVM(), "vms": list}, nil
}

func (s *Server) toolBackups(ctx context.Context) (any, error) {
	type job struct {
		ID          string     `json:"id"`
		Enabled     bool       `json:"enabled"`
		Destination string     `json:"destination"`
		Sources     []string   `json:"sources"`
		Running     bool       `json:"running"`
		Percent     float64    `json:"percent,omitempty"`
		LastRun     *time.Time `json:"lastRun"`
		LastOK      *time.Time `json:"lastOK"`
		LastError   string     `json:"lastError,omitempty"`
		Warning     string     `json:"warning,omitempty"`
		Next        *time.Time `json:"next"`
		Overdue     bool       `json:"overdue"`
	}
	list := []job{}
	for _, view := range s.opts.Backup.List() {
		list = append(list, job{ID: view.Job.ID, Enabled: view.Job.Enabled, Destination: view.Job.Destination.Type, Sources: view.Job.Sources,
			Running: view.State.Running, Percent: view.State.Percent, LastRun: view.State.LastRun, LastOK: view.State.LastOK,
			LastError: view.State.LastError, Warning: view.State.Warning, Next: view.Next, Overdue: s.opts.Backup.Overdue(view)})
	}
	return map[string]any{"jobs": list}, nil
}

func (s *Server) toolLogs(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Source   string `json:"source"`
		MinLevel string `json:"minLevel"`
		Search   string `json:"search"`
		Lines    int    `json:"lines"`
	}
	if err := toolcatalog.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	if req.Source == "all" {
		req.Source = ""
	}
	if _, known := diagnose.Sources[req.Source]; !known {
		return nil, toolcatalog.Errorf("unknown source %q", req.Source)
	}
	priority := map[string]int{"": 6, "info": 6, "warning": 4, "error": 3}
	level, known := priority[req.MinLevel]
	if !known {
		return nil, toolcatalog.Errorf("minLevel must be error, warning or info")
	}
	if len(req.Search) > 200 {
		return nil, toolcatalog.Errorf("search is too long")
	}
	logs, err := s.opts.Diagnose.Logs(ctx, diagnose.Query{Source: req.Source, Priority: level, Search: req.Search, Lines: clampLines(req.Lines)})
	if err != nil {
		return nil, err
	}
	redactor := s.redactor()
	type line struct {
		Time    time.Time `json:"time"`
		Level   string    `json:"level"`
		Source  string    `json:"source"`
		Message string    `json:"message"`
	}
	lines := []line{}
	for _, entry := range logs.Entries {
		lvl := "info"
		switch {
		case entry.Priority <= 3:
			lvl = "error"
		case entry.Priority == 4:
			lvl = "warning"
		case entry.Priority == 7:
			lvl = "debug"
		}
		lines = append(lines, line{Time: entry.Time, Level: lvl, Source: entry.Source, Message: redactor.Text(entry.Message)})
	}
	return map[string]any{"lines": lines}, nil
}
