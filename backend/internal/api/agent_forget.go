package api

import (
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/protect"
)

// Deleting an account takes along what the agent keeps of it (owner 16
// Nov 2026: "Mitlöschen", A-42): chats with their files, memory, skills,
// projects it owns alone, scheduled tasks, notes and its folder in the
// share "KI-Agent", the documents' memory and paperwork log (A-13), its
// own settings of the functions. A new account of the same name inherits
// none of it – neither files nor what names the account elsewhere: the
// list of accounts that may use the agent, "react as"/"Assist as", the
// emergency access (entry, requests, code), the access a lock of the
// ransomware guard put aside, the membership in others' projects.
//
// A shared project the account owned goes to one of its members: the
// first that is an owner of the NAS, else the first the owner added. Not
// to an owner of the NAS who was no member: the project holds the
// members' chats, and nobody outside the circle should get to read them
// by a deletion. Without members left it goes with the account.

// agentAccountData counts what is deleted with an account, for the
// question before (GET /api/v1/users/{name}/agent-data, 1.03).
type agentAccountData struct {
	// Available is false when the agent's volume is not there; the
	// counts are then 0 and the page names the parts without numbers.
	Available bool `json:"available"`
	Chats     int  `json:"chats"`
	// Projects the account owns; HandedOver of them go to a member.
	Projects   int `json:"projects"`
	HandedOver int `json:"handedOver"`
	Memories   int `json:"memories"`
	Skills     int `json:"skills"`
	Schedules  int `json:"schedules"`
	Notes      int `json:"notes"`
	// Folder: the account has a folder in the share KI-Agent.
	Folder bool `json:"folder"`
	// MailAccounts are the mailboxes of T-Mail the deletion takes along
	// (1.05; from the stick's settings in RAM, also without the agent).
	MailAccounts int `json:"mailAccounts"`
}

// agentDataOf counts the agent's data of an account. It reads the volume
// (an explicit question of an administrator, not a cyclic look).
func (s *Server) agentDataOf(name string) agentAccountData {
	store := s.agentStore()
	out := agentAccountData{}
	if s.opts.Mail != nil {
		out.MailAccounts = s.opts.Mail.MailboxCount(name)
	}
	if s.agentOpen == nil && s.agentSettings().Volume == "" || store.Available() != nil {
		return out
	}
	out.Available = true
	if chats, err := store.Chats(name); err == nil {
		out.Chats = len(chats)
	}
	if list, err := store.Projects(name); err == nil {
		out.Projects = len(list)
		for _, p := range list {
			if heir, _ := s.agentHeir(name, p); heir != "" {
				out.HandedOver++
			}
		}
	}
	if list, err := store.Memories(name); err == nil {
		out.Memories = len(list)
	}
	if list, err := store.Skills(name); err == nil {
		out.Skills = len(list)
	}
	if list, err := store.Schedules(name); err == nil {
		out.Schedules = len(list)
	}
	if dir := s.agentFolderOf(name); dir != "" {
		if st, err := os.Lstat(dir); err == nil && st.IsDir() {
			out.Folder = true
			if root, err := os.OpenRoot(dir); err == nil {
				_ = fs.WalkDir(root.FS(), "Notizen", func(p string, d fs.DirEntry, err error) error {
					if err == nil && !d.IsDir() && strings.HasSuffix(strings.ToLower(p), ".md") {
						out.Notes++
					}
					return nil
				})
				root.Close()
			}
		}
	}
	return out
}

// agentFolderOf is the account's folder in the share KI-Agent, or "" when
// there is no such share.
func (s *Server) agentFolderOf(name string) string {
	env := s.agentEnv()
	if env.shares == nil || !agentAccountName(name) {
		return ""
	}
	for _, sh := range env.shares.List() {
		if strings.EqualFold(sh.Name, agentShareName) && sh.PerUser {
			return env.shares.UserFolder(sh, name)
		}
	}
	return ""
}

// agentAccountName is a name the agent keeps files under.
func agentAccountName(name string) bool {
	return name != "" && name != agent.AssistProfile && !strings.ContainsAny(name, `/\.`) && len(name) <= 32
}

// agentHeir is the member a shared project of a deleted owner goes to, and
// the members it keeps; "" when none may use the agent.
func (s *Server) agentHeir(owner string, p agent.Project) (string, []string) {
	env := s.agentEnv()
	accounts := s.agentAccounts()
	var members []string
	for _, m := range p.Members {
		if m != owner && slices.Contains(accounts, m) && !slices.Contains(members, m) {
			members = append(members, m)
		}
	}
	if len(members) == 0 {
		return "", nil
	}
	heir := members[0]
	for _, m := range members {
		if u, ok := env.users.Get(m); ok && u.Role == auth.RoleOwner {
			heir = m
			break
		}
	}
	return heir, members
}

// agentForgetSettings takes a deleted account out of agent.json: the
// accounts that may use the agent, "react as", "Assist as" and the persons
// of the emergency access. One write, only when the account was named;
// never over a file that could not be read.
func (s *Server) agentForgetSettings(name string) {
	env := s.agentEnv()
	s.agentSettings() // loaded
	a := &s.agentConf
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.loaded {
		slog.Warn("agent settings not readable; a deleted account stays named in them", "account", name)
		return
	}
	next := a.settings
	changed := false
	if slices.Contains(next.Users, name) {
		next.Users = slices.DeleteFunc(slices.Clone(next.Users), func(n string) bool { return n == name })
		changed = true
	}
	if next.ReactAs == name {
		next.ReactAs, changed = "", true
	}
	if next.HA.AssistAs == name {
		next.HA.AssistAs, changed = "", true
	}
	if slices.ContainsFunc(next.Features.Emergency.Persons, func(p protect.Person) bool { return p.Account == name }) {
		next.Features.Emergency.Persons = slices.DeleteFunc(slices.Clone(next.Features.Emergency.Persons), func(p protect.Person) bool { return p.Account == name })
		changed = true
	}
	if !changed {
		return
	}
	if err := env.store.WriteJSON(agentSettingsFile, next); err != nil {
		slog.Error("agent settings still name a deleted account", "account", name, "err", err)
		return
	}
	a.settings = next
}

// agentForgetting serialises the removals per server: the one of a
// deletion and the one when an account of the same name is made.
var agentForgetting sync.Map // *Server → *sync.Mutex

func (s *Server) agentForgetLock() *sync.Mutex {
	v, _ := agentForgetting.LoadOrStore(s, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// agentForgetAccount removes the agent's data of a deleted account. It
// runs in the background after the account is gone (users.Delete). When
// an account of the same name was made in the meantime, that already
// removed it (agentForgetLeftovers) and the new account's data stays.
func (s *Server) agentForgetAccount(name string) {
	if !agentAccountName(name) {
		return
	}
	mu := s.agentForgetLock()
	mu.Lock()
	defer mu.Unlock()
	if _, made := s.agentEnv().users.Get(name); made {
		return
	}
	s.agentForget(name)
}

func (s *Server) agentForget(name string) {
	s.agentForgetSettings(name)
	s.agentStopRuns(name)
	s.agentMirror.mu.Lock()
	delete(s.agentMirror.state, name)
	s.agentMirror.mu.Unlock()
	agentNightNotes.Delete(name)
	docsRuntimes.Delete(docsKey{s, name})
	// Without a volume of the agent there are no files of it; opening the
	// store would choose one and write that to the stick.
	if s.agentOpen == nil && s.agentSettings().Volume == "" {
		return
	}
	s.agentHandOver(name)
	ps := s.protect()
	if err := ps.emergency.ForgetAccount(name); err != nil && !errors.Is(err, agent.ErrNoVolume) && !errors.Is(err, agent.ErrNotFound) {
		slog.Warn("emergency access of a deleted account not removed", "account", name, "err", err)
	}
	s.guardForgetAccount(name)
	// The documents (A-13) before the store: their own settings lie in the
	// account's folder of the store.
	s.docsForgetUser(name)
	if err := s.agentStore().Forget(name); err != nil && !errors.Is(err, agent.ErrNotFound) {
		slog.Warn("agent data of a deleted account not removed", "account", name, "err", err)
	}
	if dir := s.agentFolderOf(name); dir != "" {
		if st, err := os.Lstat(dir); err == nil {
			if st.Mode()&os.ModeSymlink != 0 {
				err = os.Remove(dir)
			} else {
				err = os.RemoveAll(dir)
			}
			if err != nil {
				slog.Warn("folder of a deleted account in KI-Agent not removed", "account", name, "err", err)
			}
		}
	}
	slog.Info("agent data of a deleted account removed", "account", name)
}

// agentForgetLeftovers runs when an account is made: what a deletion could
// not remove (the agent's volume was not there) must not go to the new
// account of the same name. Afterwards its folder may be made again.
func (s *Server) agentForgetLeftovers(name string) {
	if !agentAccountName(name) {
		return
	}
	mu := s.agentForgetLock()
	mu.Lock()
	defer mu.Unlock()
	s.agentForget(name)
	s.agentStore().Unforget(name)
}

// agentStopRuns ends the runs of the account and waits for them, so that
// none writes its chat again after the folder is gone.
func (s *Server) agentStopRuns(name string) {
	deadline := time.Now().Add(time.Minute)
	for {
		var left []*agentRun
		s.agentRT.mu.Lock()
		for _, run := range s.agentRT.runs {
			if run.user == name {
				left = append(left, run)
			}
		}
		s.agentRT.mu.Unlock()
		if len(left) == 0 {
			return
		}
		for _, run := range left {
			run.cancel()
		}
		if time.Now().After(deadline) {
			slog.Warn("runs of a deleted account did not end", "account", name, "runs", len(left))
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// agentHandOver gives the shared projects of a deleted owner to a member
// (see the top of the file) and takes the account out of the projects of
// others. The members' chats in a project that goes follow it.
func (s *Server) agentHandOver(name string) {
	store := s.agentStore()
	own, err := store.Projects(name)
	if err != nil {
		own = nil
	}
	for _, p := range own {
		heir, members := s.agentHeir(name, p)
		if heir == "" {
			continue
		}
		newID := p.ID
		err := store.UpdateProjects(heir, func(list []agent.Project) ([]agent.Project, error) {
			if len(list) >= maxProjects {
				return nil, errAgentListFull
			}
			if slices.ContainsFunc(list, func(q agent.Project) bool { return q.ID == newID }) {
				newID = agent.NewID()
			}
			q := p
			q.ID, q.Owner = newID, ""
			q.Members = slices.DeleteFunc(slices.Clone(members), func(m string) bool { return m == heir })
			return append(list, q), nil
		})
		if err != nil {
			slog.Warn("shared project of a deleted account not handed over", "account", name, "to", heir, "err", err)
			heir = ""
		}
		for _, m := range members {
			to := ""
			switch {
			case heir == m:
				to = newID
			case heir != "":
				to = heir + ":" + newID
			}
			s.agentMoveChats(m, name+":"+p.ID, to)
		}
	}
	for _, other := range store.Accounts() {
		if other == name || other == agent.AssistProfile {
			continue
		}
		_ = store.UpdateProjects(other, func(list []agent.Project) ([]agent.Project, error) {
			changed := false
			for i := range list {
				if slices.Contains(list[i].Members, name) {
					list[i].Members = slices.DeleteFunc(slices.Clone(list[i].Members), func(m string) bool { return m == name })
					changed = true
				}
			}
			if !changed {
				return nil, agent.ErrUnchanged
			}
			return list, nil
		})
	}
}

// agentMoveChats sets the project of the account's chats in project from
// to to ("" leaves them without one). A chat that runs keeps its project:
// its run would write the old one back.
func (s *Server) agentMoveChats(user, from, to string) {
	store := s.agentStore()
	chats, err := store.Chats(user)
	if err != nil {
		return
	}
	for _, info := range chats {
		if info.Project != from || s.agentRunning(user, info.ID) {
			continue
		}
		chat, err := store.Chat(user, info.ID)
		if err != nil {
			continue
		}
		chat.Project = to
		if err := store.SaveChat(user, chat); err != nil {
			slog.Warn("chat keeps the project of a deleted account", "account", user, "chat", chat.ID, "err", err)
		}
	}
}

// guardForgetAccount drops a deleted account from the access the
// ransomware guard put aside for a locked share: reopening must not give
// a new account of the same name the old one's right to write.
func (s *Server) guardForgetAccount(name string) {
	ps := s.protect()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	s.guardLoadLocked(ps)
	if !ps.loaded {
		return
	}
	changed := false
	for i := range ps.alerts {
		if _, ok := ps.alerts[i].Saved[name]; ok {
			saved := maps.Clone(ps.alerts[i].Saved)
			delete(saved, name)
			ps.alerts[i].Saved = saved
			changed = true
		}
	}
	if changed {
		s.guardSaveLocked(ps)
	}
}
