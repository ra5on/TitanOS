package api

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// Quick commands of the KI-Agent (0.95): what agent.ParseQuick read from a
// message becomes one step here – a service of Home Assistant for exactly
// the device that was named, or a reminder. The step runs through the
// toolbox of the account, so the released devices, the rules per device
// and the rights decide as they do for the model; nothing here knows more
// than the model's tools would.

// quickEntity is a device as ha_entities lists it.
type quickEntity struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Area   string `json:"area"`
	Policy struct {
		Aliases  []string `json:"aliases"`
		ReadOnly bool     `json:"readOnly"`
	} `json:"policy"`
}

func (e quickEntity) domain() string {
	d, _, _ := strings.Cut(e.ID, ".")
	return d
}

// quickDomains are the kinds of devices a quick command may touch, per
// intent. Locks, alarms and scripts stay with the model. Scenes and the
// target temperature of a room came with 0.95 (owner 15 Nov 2026); the
// rules per kind of device still decide whether they run without asking.
var quickDomains = map[string][]string{
	agent.QuickScene: {"scene"},
	agent.QuickTemp:  {"climate"},
	agent.QuickOn:    {"light", "switch", "fan", "input_boolean", "media_player", "scene"},
	agent.QuickOff:   {"light", "switch", "fan", "input_boolean", "media_player"},
	agent.QuickLevel: {"light"},
	agent.QuickOpen:  {"cover"},
	agent.QuickClose: {"cover"},
	agent.QuickState: {"light", "switch", "fan", "input_boolean", "media_player", "cover", "binary_sensor"},
}

// quickLights are the words for "the light(s)" of a room.
var quickLights = map[string]bool{"licht": true, "lichter": true, "lampe": true, "lampen": true, "beleuchtung": true, "light": true, "lights": true, "lamp": true, "lamps": true}

// sameWords: the same words in any order, or the same letters written
// together or apart ("Wohnzimmerlicht" and "Wohnzimmer Licht").
func sameWords(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	if strings.Join(a, "") == strings.Join(b, "") {
		return true
	}
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// quickFind names the devices an intent means: the one whose name (or
// alias, or room plus name) is exactly what was said, or all lights of the
// room that was named. Anything else – no match, two devices of the same
// name – is nothing, and the model takes over.
func quickFind(in *agent.QuickIntent, all []quickEntity) []quickEntity {
	said := agent.QuickWords(in.Name)
	if len(said) == 0 {
		return nil
	}
	domains := quickDomains[in.Kind]
	var found []quickEntity
	for _, e := range all {
		if !slices.Contains(domains, e.domain()) {
			continue
		}
		names := append([]string{e.Name}, e.Policy.Aliases...)
		hit := false
		for _, n := range names {
			words := agent.QuickWords(n)
			if sameWords(said, words) || (e.Area != "" && sameWords(said, append(agent.QuickWords(e.Area), words...))) {
				hit = true
				break
			}
		}
		if hit {
			found = append(found, e)
		}
	}
	if len(found) == 1 {
		return found
	}
	if len(found) > 1 {
		return nil
	}
	// "Wohnzimmer auf 21 Grad", "Heizung im Wohnzimmer …": the one
	// thermostat of the room named.
	if in.Kind == agent.QuickTemp {
		var room []string
		for _, w := range said {
			if !quickHeat[w] {
				room = append(room, w)
			}
		}
		for _, e := range all {
			if e.domain() == "climate" && e.Area != "" && sameWords(room, agent.QuickWords(e.Area)) {
				found = append(found, e)
			}
		}
		if len(found) == 1 {
			return found
		}
		found = nil
	}
	if group := quickRoomLights(domains, said, all); len(group) > 0 {
		return group
	}
	// Nearly the name: "Küchenlicht" for "Küche Licht". One letter may
	// differ (two in long names), numbers never, and only one device may
	// come that close.
	want := strings.Join(said, "")
	limit := 0
	switch n := len([]rune(want)); {
	case n >= 11:
		limit = 2
	case n >= 6:
		limit = 1
	}
	if limit == 0 {
		return nil
	}
	for _, e := range all {
		if !slices.Contains(domains, e.domain()) {
			continue
		}
		for _, n := range append([]string{e.Name}, e.Policy.Aliases...) {
			name := strings.Join(agent.QuickWords(n), "")
			if quickDigits(name) == quickDigits(want) && quickDistance(want, name, limit) <= limit {
				found = append(found, e)
				break
			}
		}
	}
	if len(found) == 1 {
		return found
	}
	return nil
}

// quickHeat are the words for "the heating" of a room.
var quickHeat = map[string]bool{"heizung": true, "thermostat": true, "heizkörper": true, "temperatur": true, "heating": true, "temperature": true, "radiator": true}

func quickDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// quickDistance is how many letters must change to make b of a; more than
// limit is reported as limit+1.
func quickDistance(a, b string, limit int) int {
	x, y := []rune(a), []rune(b)
	if d := len(x) - len(y); d > limit || -d > limit {
		return limit + 1
	}
	prev := make([]int, len(y)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(x); i++ {
		cur := make([]int, len(y)+1)
		cur[0] = i
		for j := 1; j <= len(y); j++ {
			cost := 1
			if x[i-1] == y[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return min(prev[len(y)], limit+1)
}

// quickRoomLights: "das Licht im Wohnzimmer" is every light of the room
// named.
func quickRoomLights(domains, said []string, all []quickEntity) []quickEntity {
	var found []quickEntity
	if !slices.Contains(domains, "light") {
		return nil
	}
	var room []string
	light := false
	for _, w := range said {
		if quickLights[w] {
			light = true
		} else {
			room = append(room, w)
		}
	}
	if !light || len(room) == 0 {
		return nil
	}
	for _, e := range all {
		if e.domain() == "light" && e.Area != "" && sameWords(room, agent.QuickWords(e.Area)) {
			found = append(found, e)
		}
	}
	if len(found) > 50 {
		return nil
	}
	return found
}

// quickPlan turns an intent and its devices into the step and the answer.
func quickPlan(in *agent.QuickIntent, found []quickEntity) (agent.QuickStep, bool) {
	if len(found) == 0 {
		return agent.QuickStep{}, false
	}
	domain := found[0].domain()
	ids := make([]string, 0, len(found))
	for _, e := range found {
		if e.domain() != domain || (in.Kind != agent.QuickState && e.Policy.ReadOnly) {
			return agent.QuickStep{}, false
		}
		ids = append(ids, e.ID)
	}
	who := found[0].Name
	if len(found) > 1 {
		if in.English {
			who = fmt.Sprintf("%d lights in %s", len(found), found[0].Area)
		} else {
			who = fmt.Sprintf("%d Lichter in %s", len(found), found[0].Area)
		}
	}
	say := func(de, en string) func(string) string {
		return func(string) string {
			if in.English {
				return who + " " + en
			}
			return who + " " + de
		}
	}
	plural := len(found) > 1
	pick := func(one, many string) string {
		if plural {
			return many
		}
		return one
	}
	call := func(service string, data map[string]any) json.RawMessage {
		args := map[string]any{"domain": domain, "service": service, "entities": ids}
		if data != nil {
			args["data"] = data
		}
		return mustJSON(args)
	}
	switch in.Kind {
	case agent.QuickScene:
		return agent.QuickStep{Tool: "ha_call", Args: call("turn_on", nil), Say: say("ist aktiviert.", "is activated.")}, true
	case agent.QuickTemp:
		if len(found) != 1 {
			return agent.QuickStep{}, false
		}
		degrees := float64(in.Value) / 10
		de := strings.Replace(strconv.FormatFloat(degrees, 'f', -1, 64), ".", ",", 1)
		return agent.QuickStep{Tool: "ha_call", Args: call("set_temperature", map[string]any{"temperature": degrees}),
			Say: say("steht auf "+de+" °C.", "is set to "+strconv.FormatFloat(degrees, 'f', -1, 64)+" °C.")}, true
	case agent.QuickOn:
		if domain == "scene" {
			return agent.QuickStep{Tool: "ha_call", Args: call("turn_on", nil), Say: say("ist aktiviert.", "is activated.")}, true
		}
		return agent.QuickStep{Tool: "ha_call", Args: call("turn_on", nil), Say: say(pick("ist an.", "sind an."), pick("is on.", "are on."))}, true
	case agent.QuickOff:
		return agent.QuickStep{Tool: "ha_call", Args: call("turn_off", nil), Say: say(pick("ist aus.", "sind aus."), pick("is off.", "are off."))}, true
	case agent.QuickLevel:
		if in.Value == 0 {
			return agent.QuickStep{Tool: "ha_call", Args: call("turn_off", nil), Say: say(pick("ist aus.", "sind aus."), pick("is off.", "are off."))}, true
		}
		return agent.QuickStep{Tool: "ha_call", Args: call("turn_on", map[string]any{"brightness_pct": in.Value}),
			Say: say(fmt.Sprintf(pick("steht auf %d %%.", "stehen auf %d %%."), in.Value), fmt.Sprintf(pick("is at %d %%.", "are at %d %%."), in.Value))}, true
	case agent.QuickOpen:
		return agent.QuickStep{Tool: "ha_call", Args: call("open_cover", nil), Say: say("fährt auf.", "is opening.")}, true
	case agent.QuickClose:
		return agent.QuickStep{Tool: "ha_call", Args: call("close_cover", nil), Say: say("fährt zu.", "is closing.")}, true
	case agent.QuickState:
		if len(found) != 1 {
			return agent.QuickStep{}, false
		}
		return agent.QuickStep{Tool: "ha_state", Args: mustJSON(map[string]any{"entity": found[0].ID}), Say: func(result string) string {
			var st struct {
				State string `json:"state"`
			}
			if json.Unmarshal([]byte(quickJSON(result)), &st) != nil || st.State == "" {
				return ""
			}
			return quickStateText(who, st.State, in.English)
		}}, true
	}
	return agent.QuickStep{}, false
}

// quickStateText says a state in words; one it has no word for goes to the
// model ("").
func quickStateText(who, state string, english bool) string {
	words := map[string][2]string{
		"on": {"ist an.", "is on."}, "off": {"ist aus.", "is off."},
		"open": {"ist offen.", "is open."}, "closed": {"ist geschlossen.", "is closed."},
		"opening": {"fährt gerade auf.", "is opening."}, "closing": {"fährt gerade zu.", "is closing."},
		"playing": {"läuft.", "is playing."}, "paused": {"ist pausiert.", "is paused."},
		"idle": {"ist an, es läuft nichts.", "is on, nothing is playing."}, "standby": {"ist in Bereitschaft.", "is in standby."},
		"unavailable": {"ist nicht erreichbar.", "is unavailable."},
	}
	w, ok := words[state]
	if !ok {
		return ""
	}
	if english {
		return who + " " + w[1]
	}
	return who + " " + w[0]
}

// quickJSON is the JSON object in the text of a tool result.
func quickJSON(text string) string {
	if i := strings.IndexByte(text, '{'); i >= 0 {
		return text[i:]
	}
	return text
}

// quickEntities reads the released devices through the account's own
// tools (ha_entities), page by page. Where the tool is missing, needs
// approval or fails there are none.
func quickEntities(ctx context.Context, tools agent.Toolbox, chat *agent.Chat) []quickEntity {
	var all []quickEntity
	for offset, pages := 0, 0; pages < 30; pages++ {
		p, err := tools.Prepare(ctx, chat, "ha_entities", mustJSON(map[string]any{"offset": offset}))
		if err != nil || p.Mode != agent.ModeAllow {
			return nil
		}
		out, err := tools.Execute(ctx, chat, p)
		if err != nil {
			return nil
		}
		var page struct {
			Entities   []quickEntity `json:"entities"`
			NextOffset int           `json:"nextOffset"`
		}
		if json.Unmarshal([]byte(quickJSON(out.Text)), &page) != nil {
			return nil
		}
		all = append(all, page.Entities...)
		if page.NextOffset <= offset {
			break
		}
		offset = page.NextOffset
	}
	return all
}

// agentQuick are the steps for the last message of the chat, if it is a
// quick command – or two or three joined by "und" – that can be read for
// certain. One part that cannot be read hands the whole message to the
// model.
func agentQuick(ctx context.Context, tools agent.Toolbox, chat *agent.Chat) []agent.QuickStep {
	if chat.Autonomy != "" || len(chat.Entries) == 0 || chat.Pending != nil {
		return nil
	}
	last := chat.Entries[len(chat.Entries)-1]
	if last.Role != "user" {
		return nil
	}
	var all []quickEntity
	read := false
	plan := func(in *agent.QuickIntent) (agent.QuickStep, bool) {
		if !read {
			all, read = quickEntities(ctx, tools, chat), true
		}
		return quickPlan(in, quickFind(in, all))
	}
	if in := agent.ParseQuick(last.Content); in != nil {
		if in.Kind == agent.QuickRemind {
			return []agent.QuickStep{quickReminder(in)}
		}
		if step, ok := plan(in); ok {
			return []agent.QuickStep{step}
		}
	}
	var steps []agent.QuickStep
	for _, in := range agent.ParseQuickAll(last.Content) {
		step, ok := plan(in)
		if !ok {
			return nil
		}
		steps = append(steps, step)
	}
	return steps
}

func quickReminder(in *agent.QuickIntent) agent.QuickStep {
	text, name := in.Text, in.Text
	if text == "" {
		text, name = "Timer", "Timer"
		if !in.English {
			text = fmt.Sprintf("Dein Timer über %s ist abgelaufen.", quickSpan(in.Value, false))
		} else {
			text = fmt.Sprintf("Your timer of %s is over.", quickSpan(in.Value, true))
		}
	}
	if r := []rune(name); len(r) > 40 {
		name = string(r[:40])
	}
	at := time.Now().Add(time.Duration(in.Value) * time.Minute).Format("15:04")
	return agent.QuickStep{Tool: "schedule_create",
		Args: mustJSON(map[string]any{"name": name, "task": text, "kind": "reminder", "every": "once", "in_minutes": in.Value}),
		Say: func(string) string {
			switch {
			case in.Text == "" && in.English:
				return fmt.Sprintf("Timer set: %s, until %s.", quickSpan(in.Value, true), at)
			case in.Text == "":
				return fmt.Sprintf("Timer gestellt: %s, bis %s Uhr.", quickSpan(in.Value, false), at)
			case in.English:
				return fmt.Sprintf("I will remind you at %s: %s", at, in.Text)
			}
			return fmt.Sprintf("Ich erinnere dich um %s Uhr: %s", at, in.Text)
		}}
}

func quickSpan(minutes int, english bool) string {
	h, m := minutes/60, minutes%60
	unit := func(n int, de1, de, en1, en string) string {
		switch {
		case english && n == 1:
			return fmt.Sprintf("%d %s", n, en1)
		case english:
			return fmt.Sprintf("%d %s", n, en)
		case n == 1:
			return fmt.Sprintf("%d %s", n, de1)
		}
		return fmt.Sprintf("%d %s", n, de)
	}
	switch {
	case h == 0:
		return unit(m, "Minute", "Minuten", "minute", "minutes")
	case m == 0:
		return unit(h, "Stunde", "Stunden", "hour", "hours")
	}
	return unit(h, "Stunde", "Stunden", "hour", "hours") + " " + unit(m, "Minute", "Minuten", "minute", "minutes")
}

// agentWork is what a run does with a new message: the quick command if
// the message is one and the owner left them on, else the model.
func (s *Server) agentWork(chat *agent.Chat) func(*agent.Runner, context.Context) error {
	return func(rn *agent.Runner, ctx context.Context) error {
		if !s.agentSettings().QuickOff {
			if steps := agentQuick(ctx, rn.Tools, chat); len(steps) > 0 {
				done, err := rn.Direct(ctx, chat, steps...)
				if err != nil || done {
					return err
				}
			}
		}
		return rn.Run(ctx, chat)
	}
}
