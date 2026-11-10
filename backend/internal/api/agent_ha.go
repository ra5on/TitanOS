package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Home Assistant tools of the KI-Agent beyond the MCP catalog (PLAN 44):
// long-term statistics and saving automations and scripts. They are in the
// area "home", so the agent's settings decide like for every tool; saving
// is a change. The version that was there before is kept in the result of
// the step, so the person (or the agent) can put it back.

func (b *agentToolbox) haTools() []*agentTool {
	hub := b.s.haHub()
	if hub == nil || !b.user.Role.CanAdminister() || len(hub.Active()) == 0 {
		return nil
	}
	// With several connections the tools take "connection".
	props := func(p map[string]any) map[string]any {
		if len(hub.All()) > 1 {
			p["connection"] = b.s.haArea().haConnectionProperty()
		}
		return p
	}
	return []*agentTool{
		{name: "ha_statistics", area: toolcatalog.AreaHome, kind: agent.KindRead,
			description: "Long-term statistics of Home Assistant entities (energy, temperature …) for days or weeks, e.g. \"power used last week per day\".",
			params: schema(props(map[string]any{
				"entities": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 10, "description": "Entity ids, e.g. sensor.energy_total."},
				"days":     intSchema("How many days back.", 1, 365),
				"period":   enum("Resolution.", "hour", "day", "week", "month"),
			}), "entities", "days"),
			own: b.haStatistics},
		{name: "ha_automation_save", area: toolcatalog.AreaHome, kind: agent.KindChange,
			description: "Create or change an automation in Home Assistant (alias, description, triggers, conditions, actions, mode as in Home Assistant's YAML, given as JSON).",
			params: schema(props(map[string]any{
				"id":     str("Id of the automation; a new short id for a new one."),
				"config": map[string]any{"type": "object", "description": "The automation: alias, description, triggers, conditions, actions, mode."},
			}), "id", "config"),
			own: b.haSave("automation")},
		{name: "ha_script_save", area: toolcatalog.AreaHome, kind: agent.KindChange,
			description: "Create or change a script in Home Assistant (alias, sequence, mode, as JSON).",
			params: schema(props(map[string]any{
				"id":     str("Id of the script, e.g. good_night."),
				"config": map[string]any{"type": "object", "description": "The script: alias, sequence, mode."},
			}), "id", "config"),
			own: b.haSave("script")},
	}
}

func (b *agentToolbox) haStatistics(ctx context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	ha, args, err := b.s.haArea().haConnectionArg(args)
	if err != nil {
		return agent.Outcome{}, err
	}
	var req struct {
		Entities []string `json:"entities"`
		Days     int      `json:"days"`
		Period   string   `json:"period"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
	}
	if req.Period == "" {
		req.Period = "day"
	}
	if req.Days < 1 || req.Days > 365 {
		return agent.Outcome{}, toolcatalog.Errorf("days: 1 to 365")
	}
	for _, e := range req.Entities {
		if !ha.AllowedCtx(ctx, e) {
			return agent.Outcome{}, toolcatalog.Errorf("%s is not released for the agent (Einstellungen › KI-Agent › Home Assistant)", e)
		}
	}
	end := time.Now()
	data, err := ha.Statistics(ctx, req.Entities, end.AddDate(0, 0, -req.Days), end, req.Period)
	if err != nil {
		return agent.Outcome{}, err
	}
	return agent.Outcome{Text: string(data)}, nil
}

func (b *agentToolbox) haSave(kind string) func(context.Context, *agent.Chat, json.RawMessage) (agent.Outcome, error) {
	env := b.s.agentEnv()
	return func(ctx context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
		ha, args, err := b.s.haArea().haConnectionArg(args)
		if err != nil {
			return agent.Outcome{}, err
		}
		var req struct {
			ID     string         `json:"id"`
			Config map[string]any `json:"config"`
		}
		if err := json.Unmarshal(args, &req); err != nil || req.Config == nil {
			return agent.Outcome{}, toolcatalog.Errorf("give an id and the config as an object")
		}
		// Marked, so the person sees in Home Assistant who wrote it.
		desc, _ := req.Config["description"].(string)
		if !strings.Contains(desc, "TitanOS-Agent") {
			req.Config["description"] = strings.TrimSpace(desc + " (vom TitanOS-Agent)")
		}
		before, err := ha.Config(ctx, kind, req.ID)
		if err != nil {
			return agent.Outcome{}, err
		}
		err = ha.SaveConfig(ctx, kind, req.ID, req.Config)
		// Not a route of TitanOS, so the activity log hears it from here.
		if env.activity != nil {
			status := http.StatusOK
			if err != nil {
				status = http.StatusBadGateway
			}
			env.activity.Record(activity.Entry{User: b.user.Name, Via: activity.ViaAgent, Action: "homeassistant_" + kind + "_save", Target: req.ID, Status: status, OK: err == nil})
		}
		if err != nil {
			return agent.Outcome{}, err
		}
		out := map[string]any{"saved": kind + "." + req.ID}
		if before != nil {
			out["previous"] = json.RawMessage(before)
			out["note"] = "The previous version is above; save it again to undo."
		}
		res, err := toolcatalog.JSON(out)
		return agent.Outcome{Text: res.Text, Note: "homeassistant", NoteText: fmt.Sprintf("%s %s", kind, req.ID)}, err
	}
}
