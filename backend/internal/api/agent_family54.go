package api

import (
	"errors"
	"regexp"
	"slices"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/buddy"
)

// Gruppe „Gemeinschaft und Familie“ (PLAN 54-G): Backup-Buddy
// (agent_family54_buddy.go) and Skill-Katalog (agent_family54_catalog.go).
// Each is off by default; switched on, the owner's answers of 54-G are the
// values ab Werk. The Familien-Tagebuch was removed with 0.69 (owner,
// 8.11.2026); its settings ("diary") in agent.json and the accounts'
// features are dropped on the next save, older versions read them as off.

// agentBuddy are the NAS-wide settings of the Backup-Buddy. The partners
// themselves (with the hashes of their tokens and the token this NAS signs
// in with there) are not here but in /config/system/agent-buddy.json,
// changed only by pairing; the page never sends them.
type agentBuddy struct {
	Enabled bool `json:"enabled,omitempty"`
	// Address is how partners reach this NAS (Tailscale name or address,
	// optionally :port). The server listens on buddy.DefaultPort (8752),
	// a fixed port the firewall knows as the service "buddy".
	Address string `json:"address,omitempty"`
	// Volume keeps the backups of the partners (.titanos/buddy/<partner>).
	Volume string `json:"volume,omitempty"`
	// ProtectDays: data of partners younger than this cannot be deleted.
	ProtectDays int `json:"protectDays,omitempty"`
	// Every: weekly (default), daily or monthly, at Weekday and Time.
	Every   string `json:"every,omitempty"`
	Weekday int    `json:"weekday,omitempty"`
	Time    string `json:"time,omitempty"`
	// Probe: monthly (default) or off; ProbeFiles how many samples.
	Probe      string `json:"probe,omitempty"`
	ProbeFiles int    `json:"probeFiles,omitempty"`
}

func (b agentBuddy) view() agentBuddy { return b.withDefaults() }

func (b agentBuddy) withDefaults() agentBuddy {
	if b.ProtectDays == 0 {
		b.ProtectDays = buddy.DefaultProtectDays
	}
	if b.Every == "" {
		b.Every = "weekly"
	}
	if b.Time == "" {
		b.Time = "02:00"
	}
	if b.Probe == "" {
		b.Probe = "monthly"
	}
	if b.ProbeFiles == 0 {
		b.ProbeFiles = 50
	}
	return b
}

// agentCatalog switches the Skill-Katalog on. Its skills come from the
// sources of the App-Store (folder skills/), there is nothing else to set.
type agentCatalog struct {
	Enabled bool `json:"enabled,omitempty"`
}

func (c agentCatalog) view() agentCatalog { return c }

var clockForm = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func (s *Server) checkFamilyNAS(f *agentFeatures, old agentFeatures) error {
	env := s.agentEnv()
	b := &f.Buddy
	if b.Address != "" {
		address, err := buddy.CheckAddress(b.Address)
		if err != nil {
			return err
		}
		b.Address = address
	}
	if b.ProtectDays != 0 && (b.ProtectDays < 7 || b.ProtectDays > 365) {
		return errors.New("buddy: protection 7 to 365 days")
	}
	if b.Every != "" && !slices.Contains([]string{"daily", "weekly", "monthly"}, b.Every) {
		return errors.New("buddy: daily, weekly or monthly")
	}
	if b.Weekday < 0 || b.Weekday > 6 || b.Time != "" && !clockForm.MatchString(b.Time) {
		return errors.New("buddy: weekday 0–6 and time HH:MM")
	}
	if b.Probe != "" && b.Probe != "monthly" && b.Probe != "off" {
		return errors.New("buddy: probe monthly or off")
	}
	if b.ProbeFiles < 0 || b.ProbeFiles > 200 {
		return errors.New("buddy: 1 to 200 sample files")
	}
	if b.Volume != "" || b.Enabled {
		if env.storage == nil || !slices.Contains(env.storage.Mounted(), b.Volume) {
			if b.Volume != old.Buddy.Volume || b.Enabled && !old.Buddy.Enabled {
				return errors.New("buddy: choose a mounted volume for the backups of the partners")
			}
		}
	}
	return nil
}

// family54 is the RAM state of the group, one per server.
type family54 struct {
	codes    buddy.Codes
	server   *buddy.Server
	listener buddy.Listener

	mu       sync.Mutex
	loaded   bool
	partners []buddyPartner
	// tick bookkeeping, only in RAM.
	lastNet   int64
	synced    string
	probeTry  map[string]int64
	jobErr    map[string]string
	addresses []string
}

var family54States sync.Map // *Server → *family54

func (s *Server) family54() *family54 {
	if v, ok := family54States.Load(s); ok {
		return v.(*family54)
	}
	v, _ := family54States.LoadOrStore(s, &family54{probeTry: map[string]int64{}, jobErr: map[string]string{}})
	return v.(*family54)
}

func (s *Server) registerAgentFamily54(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/agent/buddy", s.requireAdmin(s.handleBuddy))
	mux.HandleFunc("POST /api/v1/agent/buddy/code", s.requireAdmin(s.handleBuddyCode))
	mux.HandleFunc("DELETE /api/v1/agent/buddy/code", s.requireAdmin(s.handleBuddyCodeCancel))
	mux.HandleFunc("POST /api/v1/agent/buddy/pair", s.requireAdmin(s.handleBuddyPair))
	mux.HandleFunc("PUT /api/v1/agent/buddy/partners/{id}", s.requireAdmin(s.handleBuddyPartnerSet))
	mux.HandleFunc("DELETE /api/v1/agent/buddy/partners/{id}", s.requireAdmin(s.handleBuddyPartnerDelete))
	mux.HandleFunc("GET /api/v1/agent/catalog", s.requireAgent(s.handleCatalog))
	mux.HandleFunc("POST /api/v1/agent/catalog/install", s.requireAgent(s.handleCatalogInstall))
	mux.HandleFunc("GET /api/v1/agent/catalog/export/{id}", s.requireAgent(s.handleCatalogExport))
}

func (s *Server) agentFamily54Tick(now agentNow) {
	s.buddyTick(now)
}
