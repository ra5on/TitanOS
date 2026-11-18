package api

// What the model is told before a run: the system prompt with the account's
// memory, skills and settings, and a short state of this NAS. Moved out of
// agent_data.go (0.99), which holds the routes for projects, memory, skills
// and planned tasks.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// agentPrompt builds the system prompt of a run.
func (s *Server) agentPrompt(ctx context.Context, user auth.User, settings agentSettings, chat *agent.Chat, box *agentToolbox) string {
	env := s.agentEnv()
	in := agent.PromptInput{Account: user.Name, Admin: user.Role.CanAdminister(), Version: env.version, Now: time.Now(), Locations: settings.Locations, Origin: chat.Origin, Autonomy: chat.Autonomy}
	// 1.13: what is open in the program of the panel, as one cleaned line.
	in.Context = s.contextLine(user.Name, chat)
	if env.monitor != nil {
		in.Host = env.monitor.Info().Hostname
	}
	in.Web = settings.Policy.Mode("web_search", agent.AreaWeb, agent.KindRead) != agent.ModeDeny
	if _, ok := s.agentShare(); ok {
		in.Folder, in.Notes = true, s.agentNotes(user)
	}
	store := s.agentStore()
	// Worked out anew for every run: a memory or skill that was removed or
	// rewritten by the person no longer makes this chat ask (0.99).
	chat.OutsideSkill = false
	if chat.Project != "" {
		in.Project, _ = s.agentProject(user.Name, chat.Project)
	}
	if mems, err := store.Memories(user.Name); err == nil {
		for _, m := range mems {
			if m.Project == "" || m.Project == chat.Project {
				in.Memories = append(in.Memories, m)
			}
		}
		// The newest count most; keep the prompt small. Pinned ones stay.
		if len(in.Memories) > 60 {
			cut := len(in.Memories) - 60
			kept := in.Memories[:0:0]
			for i, m := range in.Memories {
				if i >= cut || m.Pinned {
					kept = append(kept, m)
				}
			}
			in.Memories = kept
		}
		// PLAN 90-4: what the person's message touches counts as used today.
		// Noted on the list as it is now, not on the copy read above.
		if chat != nil && memoryTouch(slices.Clone(mems), chat.Project, lastUserText(chat), time.Now()) {
			if err := store.UpdateMemories(user.Name, func(now []agent.Memory) ([]agent.Memory, bool) {
				return now, memoryTouch(now, chat.Project, lastUserText(chat), time.Now())
			}); err != nil {
				slog.Warn("memory use not noted", "err", err)
			}
		}
		// A memory saved after reading outside content (web page, file of
		// a stranger) may carry what that content wanted remembered. It
		// steers this chat like an outside skill: web tools ask first, so
		// that nothing can be sent out on the word of an old web page.
		if chat != nil && slices.ContainsFunc(in.Memories, func(m agent.Memory) bool { return m.Untrusted }) {
			chat.OutsideSkill = true
		}
	}
	if skills, err := s.agentSkills(user.Name); err == nil {
		for _, sk := range skills {
			if !sk.Disabled && !slices.Contains(settings.SkillsOff, sk.ID) {
				in.Skills = append(in.Skills, sk)
				// A skill from outside (catalog, or saved after reading outside
				// content) steers the agent like outside content: web tools
				// ask first in this chat (PLAN 54-G, security audit).
				if sk.Untrusted && chat != nil {
					chat.OutsideSkill = true
				}
			}
		}
	}
	in.Snapshot = s.agentSnapshot(ctx, box)
	// 1.10: what is not there for this account, so that "none" is not
	// said where the agent may simply not look.
	in.Missing = slices.Sorted(maps.Keys(box.Missing()))
	in.Instruction = settings.Instruction
	if p, err := store.Profile(user.Name); err == nil {
		in.Personal = p.Instruction
	}
	// PLAN 53: personality and memory settings of the account.
	if ini, err := s.agentInitiativeOf(user.Name); err == nil {
		in.Personal = strings.TrimSpace(in.Personal + "\n" + agentStyle(ini))
	}
	in.Map = s.mapPrompt(user)
	return agent.BuildPrompt(in)
}

// agentSnapshot is a short state report: what exists on this NAS, so the
// model knows names before it calls a tool. Only names and states.
func (s *Server) agentSnapshot(ctx context.Context, box *agentToolbox) string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	readTool := func(name string) map[string]any {
		t, ok := box.tools[name]
		if !ok || t.read == nil || box.mode(t, t.kind) == agent.ModeDeny {
			return nil
		}
		res, err := t.read(ctx, json.RawMessage("{}"))
		if err != nil {
			return nil
		}
		var out map[string]any
		if json.Unmarshal([]byte(res.Text), &out) != nil {
			return nil
		}
		return out
	}
	names := func(v any, field, state string) []string {
		list, _ := v.([]any)
		var out []string
		for _, e := range list {
			m, _ := e.(map[string]any)
			n, _ := m[field].(string)
			if n == "" {
				continue
			}
			if st, _ := m[state].(string); st != "" {
				n += " (" + st + ")"
			}
			out = append(out, n)
			if len(out) >= 40 {
				out = append(out, "…")
				break
			}
		}
		return out
	}
	var b strings.Builder
	line := func(label string, items []string) {
		if len(items) > 0 {
			fmt.Fprintf(&b, "%s: %s\n", label, strings.Join(items, ", "))
		}
	}
	if st := readTool("storage_overview"); st != nil {
		line("Volumes", names(st["volumes"], "name", "state"))
	}
	if sh := readTool("shares_list"); sh != nil {
		line("Shares (root share/<name>)", names(sh["shares"], "name", ""))
	} else if sh := readTool("my_shares"); sh != nil {
		line("Your shares (root share/<name>)", names(sh["shares"], "name", ""))
	}
	if d := readTool("docker_list"); d != nil {
		line("Docker containers", names(d["containers"], "name", "state"))
	}
	if v := readTool("vm_list"); v != nil {
		line("VMs", names(v["vms"], "name", "state"))
	}
	if l := readTool("lxc_list"); l != nil {
		line("System containers (LXC)", names(l["containers"], "name", "state"))
	}
	if b.Len() == 0 {
		return ""
	}
	return "Right now:\n" + b.String()
}
