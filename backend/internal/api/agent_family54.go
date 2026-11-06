package api

import (
	"encoding/base64"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/buddy"
)

// Gruppe „Gemeinschaft und Familie“ (PLAN 54-G): Backup-Buddy
// (agent_family54_buddy.go), Skill-Katalog (agent_family54_catalog.go) and
// Familien-Tagebuch (agent_family54_diary.go). Each is off by default;
// switched on, the owner's answers of 54-G are the values ab Werk.

// agentBuddy are the NAS-wide settings of the Backup-Buddy. The partners
// themselves (with the hashes of their tokens and the token this NAS signs
// in with there) are not here but in /config/system/agent-buddy.json,
// changed only by pairing; the page never sends them.
type agentBuddy struct {
	Enabled bool `json:"enabled,omitempty"`
	// Address is how partners reach this NAS (Tailscale name or address,
	// optionally :port); Port where the server of this NAS listens.
	Address string `json:"address,omitempty"`
	Port    int    `json:"port,omitempty"`
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
	if b.Port == 0 {
		b.Port = buddy.DefaultPort
	}
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

// agentCatalog are the settings of the Skill-Katalog: where the signed
// index lies and the ed25519 key it is signed with. Both empty ab Werk;
// the owner decides hosting and key.
type agentCatalog struct {
	Enabled bool   `json:"enabled,omitempty"`
	URL     string `json:"url,omitempty"`
	Key     string `json:"key,omitempty"`
}

func (c agentCatalog) view() agentCatalog { return c }

// agentDiaryNAS switches the Familien-Tagebuch on for the agent's accounts.
type agentDiaryNAS struct {
	Enabled bool `json:"enabled,omitempty"`
}

func (d agentDiaryNAS) view() agentDiaryNAS { return d }

// agentDiaryMine are an account's own sources of the diary.
type agentDiaryMine struct {
	// Off lists sources switched off: chats, reminders, home, photos,
	// immich, frigate. Immich and Frigate count only when set up.
	Off       []string `json:"off,omitempty"`
	ImmichURL string   `json:"immichUrl,omitempty"`
	// ImmichKey is a secret: never sent back, only ImmichKeySet.
	ImmichKey    string `json:"immichKey,omitempty"`
	ImmichKeySet bool   `json:"immichKeySet,omitempty"`
	// ClearImmichKey removes the stored key (from the page).
	ClearImmichKey bool   `json:"clearImmichKey,omitempty"`
	FrigateURL     string `json:"frigateUrl,omitempty"`
	// Photos is a folder of the account with photos (EXIF dates).
	Photos *agentFolderRef `json:"photos,omitempty"`
}

func (d agentDiaryMine) view() agentDiaryMine {
	d.ImmichKeySet, d.ImmichKey, d.ClearImmichKey = d.ImmichKey != "", "", false
	return d
}

var diarySources = []string{"chats", "reminders", "home", "photos", "immich", "frigate"}

func (d agentDiaryMine) on(source string) bool { return !slices.Contains(d.Off, source) }

var clockForm = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func (s *Server) checkFamilyNAS(f *agentFeatures, old agentFeatures) error {
	b := &f.Buddy
	if b.Port != 0 && (b.Port < 1024 || b.Port > 65535) {
		return errors.New("buddy: port 1024 to 65535")
	}
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
		if s.opts.Storage == nil || !slices.Contains(s.opts.Storage.Mounted(), b.Volume) {
			if b.Volume != old.Buddy.Volume || b.Enabled && !old.Buddy.Enabled {
				return errors.New("buddy: choose a mounted volume for the backups of the partners")
			}
		}
	}
	c := &f.Catalog
	c.URL, c.Key = strings.TrimSpace(c.URL), strings.TrimSpace(c.Key)
	if c.URL != "" {
		u, err := url.Parse(c.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(c.URL) > 2048 {
			return errors.New("catalog: an https address")
		}
	}
	if c.Key != "" {
		if raw, err := base64.StdEncoding.DecodeString(c.Key); err != nil || len(raw) != 32 {
			return errors.New("catalog: the public key is 32 bytes ed25519, base64")
		}
	}
	return nil
}

func (s *Server) checkDiaryMine(user string, d *agentDiaryMine, old agentDiaryMine) error {
	d.Off = slices.DeleteFunc(slices.Clone(d.Off), func(v string) bool { return !slices.Contains(diarySources, v) })
	slices.Sort(d.Off)
	d.Off = slices.Compact(d.Off)
	d.ImmichKeySet = false
	for _, raw := range []*string{&d.ImmichURL, &d.FrigateURL} {
		*raw = strings.TrimRight(strings.TrimSpace(*raw), "/")
		if *raw == "" {
			continue
		}
		u, err := url.Parse(*raw)
		if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || len(*raw) > 512 {
			return errors.New("diary: Immich and Frigate as http(s) address")
		}
	}
	d.ImmichKey = strings.TrimSpace(d.ImmichKey)
	if d.ImmichKey == "" && !d.ClearImmichKey {
		d.ImmichKey = old.ImmichKey
	}
	if d.ImmichURL == "" {
		d.ImmichKey = ""
	}
	if len(d.ImmichKey) > 256 || strings.ContainsAny(d.ImmichKey, "\r\n") {
		return errors.New("diary: Immich key")
	}
	d.ClearImmichKey = false
	if d.Photos != nil {
		if d.Photos.Root == "" {
			d.Photos = nil
			return nil
		}
		if err := s.diaryFolderAllowed(user, *d.Photos); err != nil {
			return err
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
	addresses []string
}

var family54States sync.Map // *Server → *family54

func (s *Server) family54() *family54 {
	if v, ok := family54States.Load(s); ok {
		return v.(*family54)
	}
	v, _ := family54States.LoadOrStore(s, &family54{probeTry: map[string]int64{}})
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

func (b *agentToolbox) family54Tools() []*agentTool {
	if !b.settings.Features.Diary.Enabled {
		return nil
	}
	return []*agentTool{b.diaryTool()}
}
