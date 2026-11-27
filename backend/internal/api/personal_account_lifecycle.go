package api

import (
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"io/fs"
	"log/slog"
	"strings"
)

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
	// CalendarEvents and Contacts are the events (calendar files) and
	// contacts (groups not counted) of T-Kalender and Kontakte the deletion
	// takes along (1.11, TECHNIK 5.7). Both are left out while they cannot
	// be counted – no data volume, the account's folder on another volume –
	// and CalendarUnknown says so: never 0 for data that is not known.
	CalendarEvents  *int `json:"calendarEvents,omitempty"`
	Contacts        *int `json:"contacts,omitempty"`
	CalendarUnknown bool `json:"calendarUnknown,omitempty"`
	// Reminders (1.13) are counted like the events; left out when they
	// cannot be (CalendarUnknown).
	Reminders *int `json:"reminders,omitempty"`
}

func (s *Server) countAccountApps(name string) agentAccountData {
	out := agentAccountData{}
	if s.opts.Mail != nil {
		out.MailAccounts = s.opts.Mail.MailboxCount(name)
	}
	// T-Kalender and Kontakte live on the volume of the system data, not on
	// the agent's: counted here whatever the agent's volume says.
	if s.opts.PIM != nil {
		if events, contacts, err := s.opts.PIM.Counts(name); err == nil {
			out.CalendarEvents, out.Contacts = &events, &contacts
		} else {
			out.CalendarUnknown = true
		}
		if reminders, err := s.opts.PIM.CountTasks(name); err == nil {
			out.Reminders = &reminders
		} else {
			out.CalendarUnknown = true
		}
	}
	return out
}
func (s *Server) personalDataOf(user auth.User) agentAccountData {
	out := s.countAccountApps(user.Name)
	_ = s.personalTxn(user, false, func(a *personalAccount) error {
		out.Available = true
		out.Chats = len(a.Chats)
		out.Memories = len(a.Memory)
		out.Skills = len(a.Skills)
		out.Schedules = len(a.Watches)
		return nil
	})
	return out
}

// The storage key identifies the deleted account incarnation, including when
// a replacement account with the same login is created during the deletion.
func (s *Server) forgetPersonalAccount(user auth.User) {
	s.cancelPersonalRuns(user.Name, nil)
	s.personal.mu.Lock()
	defer s.personal.mu.Unlock()
	root, release, err := s.personalRoot()
	if err != nil {
		return
	} // Detached volumes stay inaccessible to replacement accounts.
	defer release()
	defer root.Close()
	key := personalKey(user)
	delete(s.personal.known, key)
	delete(s.personal.watchDue, key)
	dir, err := root.Open(".")
	if err != nil {
		return
	}
	defer dir.Close()
	for {
		entries, readErr := dir.ReadDir(128)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if name != key+".json" && name != key+".json.prev" && !(strings.HasPrefix(name, key+"-") && strings.HasSuffix(name, ".pdf")) {
				continue
			}
			if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("could not remove personal Agent account file", "error", err)
			}
		}
		if readErr != nil {
			break
		}
	}
}
