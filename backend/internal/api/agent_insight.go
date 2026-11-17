package api

// Reading tools of the agent for what happened on the NAS (1.10, V7,
// owner 17 Nov 2026): the activity log, the monitoring history and the
// capacity forecast (area "Verlauf"), events of the cameras without
// pictures (area "NVR"), the logbook of Home Assistant, the release notes
// of the running version, and deleting a planned task. All only read
// (except schedule_delete, the agent's own data), only for owners and
// administrators, each under the rights of the agent; none is part of the
// MCP catalog. An empty answer says why when it is not "nothing happened".

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
	"github.com/ra5on/TitanOS/backend/internal/version"
)

const whereMonitoring = "Einstellungen › Monitoring"

// insightTools are the reading tools of 1.10 this account gets.
func (b *agentToolbox) insightTools() []*agentTool {
	admin := b.user.Role.CanAdminister()
	if !admin {
		return nil
	}
	read := func(name, area, description string, params map[string]any, untrusted bool, run func(context.Context, json.RawMessage) (any, error)) *agentTool {
		return &agentTool{name: name, area: area, kind: agent.KindRead, description: description, params: params, untrusted: untrusted,
			own: func(ctx context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
				if err := toolcatalog.ValidateArguments(params, args); err != nil {
					return agent.Outcome{}, err
				}
				out, err := run(ctx, args)
				if err != nil {
					return agent.Outcome{}, err
				}
				res, err := toolcatalog.JSON(out)
				return agent.Outcome{Text: res.Text}, err
			}}
	}
	env := b.s.agentEnv()
	var tools []*agentTool
	if env.activity != nil {
		tools = append(tools, read("activity_log", agent.AreaHistory,
			"The activity log of the NAS: who changed or did what, when, and how (web, app token, MCP, KI-Agent, Home Assistant, system); newest first. For \"wer hat … gelöscht?\", \"was hat der Agent geändert?\", \"was war heute Nacht?\".",
			schema(map[string]any{
				"since":   str(momentHelp + " 24 hours back if left out."),
				"until":   str(momentHelp),
				"account": str("Only this account."),
				"via":     enum("Only what came this way.", "web", "token", "mcp", "agent", "homeassistant", "system", "cluster", "ssh"),
				"search":  str("Only entries whose action or target contains this text."),
				"failed":  map[string]any{"type": "boolean", "description": "Only what failed."},
				"limit":   intSchema("How many, 50 if left out.", 1, 200),
			}), true, b.activityLog))
	}
	if env.monitoring != nil {
		tools = append(tools,
			read("monitoring_history", agent.AreaHistory,
				"How busy the NAS was over time: CPU, memory, disk reads and writes with latency, network – average, highest and when, per hour over the last day, week or month. For \"warum ist das NAS langsam?\", \"seit wann?\".",
				schema(map[string]any{"hours": map[string]any{"type": "integer", "enum": []int{24, 168, 720}, "description": "24 (day, default), 168 (week) or 720 (month)."}}), false, b.monitoringHistory),
			read("capacity_forecast", agent.AreaHistory,
				"When the volumes are full: used and total, growth per day and the days until 90 % and until full, from the monitoring history of the last 30 days.",
				schema(nil), false, b.capacityForecast))
	}
	if cams := b.s.nvrArea().nvr; cams != nil && len(cams.CameraIDs()) > 0 {
		tools = append(tools, read("nvr_events", agent.AreaNVR,
			"What the cameras of the NVR saw: which camera, what (person, car, dog …), when, in which zone – no pictures. Newest first. For \"wer war an der Haustür?\".",
			schema(map[string]any{
				"since":  str(momentHelp + " 24 hours back if left out."),
				"until":  str(momentHelp),
				"camera": str("Name or ID of one camera, all if left out."),
				"label":  str("Only this kind of object, e.g. person, car."),
				"limit":  intSchema("How many, 50 if left out.", 1, 100),
			}), true, b.nvrEvents))
	}
	if hub := b.s.haHub(); hub != nil && len(hub.Active()) > 0 {
		props := map[string]any{
			"since":  str(momentHelp + " 24 hours back if left out; at most 24 hours."),
			"until":  str(momentHelp),
			"entity": str("Only this released entity, e.g. automation.licht_aus."),
		}
		if len(hub.All()) > 1 {
			props["connection"] = b.s.haArea().haConnectionProperty()
		}
		cfg := map[string]any{"entity": str("The automation or script, e.g. automation.flur_licht or script.gute_nacht.")}
		if len(hub.All()) > 1 {
			cfg["connection"] = b.s.haArea().haConnectionProperty()
		}
		tools = append(tools, read("ha_config_read", "home",
			"The settings of one released automation or script of Home Assistant (triggers, conditions, actions), to read it before changing it with ha_automation_save or ha_script_save.",
			schema(cfg, "entity"), true, b.haConfigRead))
		tools = append(tools, read("ha_logbook", "home",
			"The logbook of Home Assistant for up to 24 hours: which automation ran, which device changed, when – only released entities. For \"was lief heute?\", \"welche Automationen liefen?\".",
			schema(props), true, b.haLogbook))
	}
	tools = append(tools, read("release_notes", "updates",
		"The release notes of TitanOS: what is new and fixed in the running version (or in another of the recent ones). For \"was ist neu?\".",
		schema(map[string]any{"version": str("A version such as 1.09; the running one if left out.")}), false, b.releaseNotes))
	return tools
}

// window reads since/until of a call; since defaults to back from now.
func window(args json.RawMessage, back time.Duration) (since, until time.Time, err error) {
	var req struct{ Since, Until string }
	_ = json.Unmarshal(args, &req)
	now := time.Now()
	if since, err = parseMoment(req.Since, now); err != nil {
		return since, until, toolcatalog.Errorf("since: %v", err)
	}
	if until, err = parseMoment(req.Until, now); err != nil {
		return since, until, toolcatalog.Errorf("until: %v", err)
	}
	if until.IsZero() {
		until = now
	}
	if since.IsZero() {
		since = until.Add(-back)
	}
	if !until.After(since) {
		return since, until, toolcatalog.Errorf("until must lie after since")
	}
	return since, until, nil
}

func (b *agentToolbox) activityLog(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Account, Via, Search string
		Failed               bool
		Limit                int
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, toolcatalog.Errorf("invalid arguments")
	}
	since, until, err := window(args, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit < 1 || limit > 200 {
		limit = 50
	}
	days := int(math.Ceil(time.Since(since).Hours()/24)) + 1
	f := activity.Filter{Days: min(max(days, 1), 365), User: req.Account, Via: req.Via, Text: req.Search, Failed: req.Failed, Limit: 1000}
	if until.Before(time.Now()) {
		f.Before = until
	}
	entries, more, stored, err := b.s.agentEnv().activity.Query(ctx, f)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return map[string]any{"entries": []any{}, "hidden": true, "why": "the activity log cannot be read right now (" + err.Error() + "); this is no sign that nothing happened"}, nil
	}
	type line struct {
		At     string `json:"at"`
		User   string `json:"user"`
		Via    string `json:"via"`
		Action string `json:"action"`
		Target string `json:"target,omitempty"`
		OK     bool   `json:"ok"`
	}
	out := []line{}
	total := 0
	for _, e := range entries {
		if e.At.Before(since) || e.At.After(until) {
			continue
		}
		total++
		if len(out) < limit {
			// No addresses: who did it is the account and the way.
			out = append(out, line{e.At.Local().Format("2006-01-02 15:04:05"), e.User, e.Via, e.Action, e.Target, e.OK})
		}
	}
	result := map[string]any{"since": since.Local().Format("2006-01-02 15:04"), "until": until.Local().Format("2006-01-02 15:04"), "total": total, "entries": out}
	if total > len(out) || more {
		result["note"] = "More entries than shown: narrow it with since/until, account, via or search."
	}
	if !stored {
		result["note"] = "The activity log is only kept in memory until a volume holds it: entries from before the last start are missing."
	}
	return result, nil
}

// monitoringOff is the answer while the monitoring history is switched off.
func monitoringOff() map[string]any {
	return map[string]any{"hidden": true, "why": "The monitoring history is switched off on this NAS, so there is no history to read – this is no sign that all was quiet. It can be switched on under " + whereMonitoring + "; system_status shows the values of now."}
}

func (b *agentToolbox) monitoringHistory(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct{ Hours int }
	_ = json.Unmarshal(args, &req)
	if req.Hours == 0 {
		req.Hours = 24
	}
	m := b.s.agentEnv().monitoring
	if !m.Status().Settings.Enabled {
		return monitoringOff(), nil
	}
	samples, err := m.History(ctx, req.Hours)
	if err != nil {
		return nil, err
	}
	return summariseHistory(samples, req.Hours), nil
}

// summariseHistory gives per series the average, the highest value with
// its time, and the hourly averages (for a week and a month per 6 hours
// and per day), so that a small model sees the shape without every sample.
func summariseHistory(samples []monitoring.Sample, hours int) map[string]any {
	if len(samples) == 0 {
		return map[string]any{"hours": hours, "samples": 0, "note": "No samples yet in this time; the history starts when monitoring was switched on."}
	}
	step := time.Hour
	switch hours {
	case 168:
		step = 6 * time.Hour
	case 720:
		step = 24 * time.Hour
	}
	type stat struct {
		Unit    string    `json:"unit"`
		Average float64   `json:"average"`
		Highest float64   `json:"highest"`
		At      string    `json:"highestAt"`
		Steps   []float64 `json:"steps"`
		sum     float64
		n       int
		buckets map[int64][2]float64
	}
	series := map[string]*stat{}
	first := samples[0].At.Truncate(step)
	add := func(name, unit string, at time.Time, v float64) {
		s := series[name]
		if s == nil {
			s = &stat{Unit: unit, buckets: map[int64][2]float64{}}
			series[name] = s
		}
		s.sum += v
		s.n++
		if v > s.Highest || s.n == 1 {
			s.Highest, s.At = v, at.Local().Format("2006-01-02 15:04")
		}
		k := int64(at.Sub(first) / step)
		bk := s.buckets[k]
		s.buckets[k] = [2]float64{bk[0] + v, bk[1] + 1}
	}
	gaps := 0
	for _, s := range samples {
		if s.Gap {
			gaps++
			continue
		}
		add("cpu", "%", s.At, s.CPU)
		if s.RAMTotal > 0 {
			add("memory", "% used", s.At, 100*float64(s.RAMTotal-s.RAMAvailable)/float64(s.RAMTotal))
		}
		for _, d := range s.Disks {
			add("disk "+d.Name+" read", "MB/s", s.At, d.ReadBPS/1e6)
			add("disk "+d.Name+" write", "MB/s", s.At, d.WriteBPS/1e6)
			if d.LatencyMS != nil {
				add("disk "+d.Name+" latency", "ms", s.At, *d.LatencyMS)
			}
		}
		for _, n := range s.Networks {
			add("network "+n.Name+" in", "MB/s", s.At, n.Receive/1e6)
			add("network "+n.Name+" out", "MB/s", s.At, n.Send/1e6)
		}
	}
	round := func(v float64) float64 { return math.Round(v*10) / 10 }
	last := int64(samples[len(samples)-1].At.Sub(first) / step)
	out := map[string]any{}
	for name, s := range series {
		s.Average, s.Highest = round(s.sum/float64(s.n)), round(s.Highest)
		for k := int64(0); k <= last; k++ {
			bk := s.buckets[k]
			if bk[1] == 0 {
				s.Steps = append(s.Steps, -1)
				continue
			}
			s.Steps = append(s.Steps, round(bk[0]/bk[1]))
		}
		out[name] = s
	}
	result := map[string]any{"hours": hours, "from": first.Local().Format("2006-01-02 15:04"), "step": step.String(), "samples": len(samples), "series": out,
		"note": "steps are the averages per step from \"from\" on; -1 is a step without samples."}
	if gaps > 0 {
		result["gaps"] = gaps
	}
	return result
}

func (b *agentToolbox) capacityForecast(ctx context.Context, _ json.RawMessage) (any, error) {
	m := b.s.agentEnv().monitoring
	if !m.Status().Settings.Enabled {
		return monitoringOff(), nil
	}
	samples, err := m.History(ctx, 720)
	if err != nil {
		return nil, err
	}
	type volume struct {
		Volume     string   `json:"volume"`
		UsedGB     float64  `json:"usedGB"`
		TotalGB    float64  `json:"totalGB"`
		PerDayGB   float64  `json:"growthPerDayGB"`
		DaysTo90   *float64 `json:"daysTo90,omitempty"`
		DaysToFull *float64 `json:"daysToFull,omitempty"`
		Warning    bool     `json:"warning,omitempty"`
		Hours      int      `json:"hoursOfData"`
		Known      bool     `json:"known"`
	}
	list := []volume{}
	for _, t := range monitoring.CapacityTrends(samples, time.Now()) {
		gb := func(v float64) float64 { return math.Round(v/1e8) / 10 }
		list = append(list, volume{t.Volume, gb(float64(t.Used)), gb(float64(t.Total)), gb(t.DailyBytes), t.DaysTo90, t.DaysToFull, t.Warning, t.Hours, t.Known})
	}
	result := map[string]any{"volumes": list, "note": "known is false when there is too little history for a forecast; no daysToFull means it does not grow."}
	if len(list) == 0 {
		result["note"] = "No volume in the history yet; a forecast needs some days of monitoring."
	}
	return result, nil
}

// nvrMode: camera events ask first unless the person allowed the tool or
// the area NVR expressly (owner 17 Nov 2026: "ab Werk mit Nachfrage");
// "Alles ohne Nachfrage" is not enough, as with running code.
func (b *agentToolbox) nvrMode(tool, mode string) string {
	p := b.settings.Policy
	if p.Tools[tool] == agent.ModeAllow || (p.Tools[tool] == "" && p.Areas[agent.AreaNVR] == agent.ModeAllow) {
		return mode
	}
	return agent.Stricter(mode, agent.ModeAsk)
}

func (b *agentToolbox) nvrEvents(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Camera, Label string
		Limit         int
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, toolcatalog.Errorf("invalid arguments")
	}
	since, until, err := window(args, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	area := b.s.nvrArea()
	cameras := area.camerasWith(b.user, nvr.RightRecordings)
	names := map[string]string{}
	for _, c := range area.nvr.Cameras() {
		names[c.ID] = c.Name
	}
	if req.Camera != "" {
		want := strings.ToLower(strings.TrimSpace(req.Camera))
		cameras = slices.DeleteFunc(cameras, func(id string) bool { return strings.ToLower(id) != want && strings.ToLower(names[id]) != want })
		if len(cameras) == 0 {
			known := []string{}
			for _, id := range area.camerasWith(b.user, nvr.RightRecordings) {
				known = append(known, cmpName(names[id], id))
			}
			return nil, toolcatalog.Errorf("there is no camera %q; cameras: %s", req.Camera, strings.Join(known, ", "))
		}
	}
	limit := req.Limit
	if limit < 1 || limit > 100 {
		limit = 50
	}
	filter := nvr.EventFilter{Cameras: cameras, Limit: limit, Account: b.user.Name, From: float64(since.Unix()), To: float64(until.Unix())}
	if req.Label != "" {
		filter.Labels = []string{req.Label}
	}
	events, next, err := area.nvr.Events(ctx, filter)
	if err != nil {
		return nil, err
	}
	type event struct {
		Camera string   `json:"camera"`
		What   string   `json:"what"`
		Who    string   `json:"who,omitempty"`
		Plate  string   `json:"plate,omitempty"`
		Start  string   `json:"start"`
		End    string   `json:"end,omitempty"`
		Zones  []string `json:"zones,omitempty"`
		Seen   bool     `json:"seen"`
	}
	at := func(sec float64) string {
		return time.Unix(int64(sec), 0).Local().Format("2006-01-02 15:04:05")
	}
	out := []event{}
	for _, e := range events {
		ev := event{Camera: cmpName(names[e.Camera], e.Camera), What: e.Label, Start: at(e.Start), Zones: e.Zones, Seen: e.Reviewed}
		if e.End != nil {
			ev.End = at(*e.End)
		}
		// Names of faces and plates only with their own switch.
		if b.settings.NVRNames {
			ev.Who, ev.Plate = e.SubLabel, e.Plate
		}
		out = append(out, ev)
	}
	result := map[string]any{"since": since.Local().Format("2006-01-02 15:04"), "until": until.Local().Format("2006-01-02 15:04"), "events": out}
	if next > 0 {
		result["note"] = fmt.Sprintf("More events before %s: call again with until=%s.", at(next), time.Unix(int64(next), 0).Local().Format("2006-01-02T15:04:05"))
	}
	if !b.settings.NVRNames {
		result["names"] = "Names of known faces and plates are not given to the agent (switch under " + whereRights + " › NVR, off from the factory). Pictures never are."
	}
	return result, nil
}

func cmpName(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

func (b *agentToolbox) haLogbook(ctx context.Context, args json.RawMessage) (any, error) {
	ha, rest, err := b.s.haArea().haConnectionArg(args)
	if err != nil {
		return nil, err
	}
	if !ha.Active() {
		return nil, homeassistant.ErrOff
	}
	var req struct{ Entity string }
	_ = json.Unmarshal(rest, &req)
	since, until, err := window(rest, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	if until.Sub(since) > 24*time.Hour {
		since = until.Add(-24 * time.Hour)
	}
	book, err := ha.Logbook(ctx, since, until, req.Entity)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"since": since.Local().Format("2006-01-02 15:04"), "until": until.Local().Format("2006-01-02 15:04"), "total": len(book.Entries), "entries": book.Entries}
	if book.Hidden {
		result["hidden"] = true
		result["why"] = "Lines of devices that are not released to the agent are left out (no count). They can be released under " + whereHA + "."
	}
	if len(book.Entries) == 0 && !book.Hidden {
		result["note"] = "Nothing in the logbook of released devices in this time."
	}
	return result, nil
}

// haConfigRead reads the configuration of a released automation or script
// (V15: an automation was changed without reading it first).
func (b *agentToolbox) haConfigRead(ctx context.Context, args json.RawMessage) (any, error) {
	ha, rest, err := b.s.haArea().haConnectionArg(args)
	if err != nil {
		return nil, err
	}
	if !ha.Active() {
		return nil, homeassistant.ErrOff
	}
	var req struct{ Entity string }
	_ = json.Unmarshal(rest, &req)
	kind, object, _ := strings.Cut(req.Entity, ".")
	if (kind != "automation" && kind != "script") || object == "" {
		return nil, toolcatalog.Errorf("entity must be an automation or a script, e.g. automation.flur_licht")
	}
	if !ha.AllowedCtx(ctx, req.Entity) {
		return nil, homeassistant.ErrNotAllowed
	}
	id := object
	if kind == "automation" {
		st, err := ha.State(ctx, req.Entity)
		if err != nil {
			return nil, err
		}
		id = fmt.Sprint(st.Attributes["id"])
		if st.Attributes["id"] == nil || id == "" {
			return map[string]any{"entity": req.Entity, "note": "This automation has no id: it is written in YAML, not made in the editor of Home Assistant, and cannot be read or changed from here."}, nil
		}
	}
	config, err := ha.Config(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return map[string]any{"entity": req.Entity, "note": "Home Assistant has no editable configuration for it (made in YAML or a blueprint)."}, nil
	}
	return map[string]any{"entity": req.Entity, "id": id, "config": config}, nil
}

func (b *agentToolbox) releaseNotes(_ context.Context, args json.RawMessage) (any, error) {
	var req struct{ Version string }
	_ = json.Unmarshal(args, &req)
	manual := b.s.manual
	running := b.version
	wanted := strings.TrimPrefix(strings.TrimSpace(req.Version), "v")
	file := running
	if wanted != "" {
		file = releaseFile(wanted)
	}
	text, ok := manual.Release(file)
	available := []string{}
	for _, v := range manual.Releases() {
		available = append(available, version.Display(v))
	}
	if !ok {
		why := "There are no release notes for " + version.Display(file) + " in this build"
		if wanted == "" {
			why = "There are no release notes for the running version " + version.Display(running) + " in this build (a build for development)"
		}
		return map[string]any{"running": version.Display(running), "available": available, "hidden": true,
			"why": why + ". Do not make up what is new; say so, or read one of the available versions."}, nil
	}
	return map[string]any{"running": version.Display(running), "version": version.Display(file), "available": available, "text": text}, nil
}

// releaseFile turns a shown version ("1.09", "0.100") into the one of a
// tag ("1.9.0", "0.100.0"); a version with three parts stays.
func releaseFile(shown string) string {
	parts := strings.Split(shown, ".")
	if len(parts) == 3 {
		return shown
	}
	if len(parts) != 2 {
		return shown
	}
	minor := strings.TrimLeft(parts[1], "0")
	if minor == "" {
		minor = "0"
	}
	if parts[0] == "0" {
		minor = parts[1]
	}
	return parts[0] + "." + minor + ".0"
}

// scheduleDeleteTool lets the agent take back a planned task or reminder
// of the account (V15: "Lösch die Erinnerung wieder").
func (b *agentToolbox) scheduleDeleteTool() *agentTool {
	return &agentTool{name: "schedule_delete", area: agent.AreaAgent, kind: agent.KindAgent,
		description: "Delete a planned task or reminder of the person by its id from schedule_list.",
		params:      schema(map[string]any{"id": str("ID as schedule_list shows it.")}, "id"),
		own: func(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
			var req struct{ ID string }
			if err := json.Unmarshal(args, &req); err != nil || req.ID == "" {
				return agent.Outcome{}, errors.New("id is missing")
			}
			name := ""
			err := b.s.agentStore().UpdateSchedules(b.user.Name, func(list []agent.Schedule) ([]agent.Schedule, error) {
				i := slices.IndexFunc(list, func(sc agent.Schedule) bool { return sc.ID == req.ID })
				if i < 0 {
					return nil, agent.ErrNotFound
				}
				name = list[i].Name
				return slices.Delete(list, i, i+1), nil
			})
			if errors.Is(err, agent.ErrNotFound) {
				return agent.Outcome{}, fmt.Errorf("there is no planned task %q; schedule_list shows them", req.ID)
			}
			if err != nil {
				return agent.Outcome{}, err
			}
			return agent.Outcome{Text: "Deleted the planned task " + name + "."}, nil
		}}
}
