package agent

import (
	"errors"
	"slices"
	"strings"
)

// Rights per kind of action (1.13, owner 18 Nov 2026, Q27–Q29): what the
// agent does with mail, events, contacts and reminders is set per action,
// not per tool: "Nur Vorschläge" (suggest: the tool makes an editable draft
// and writes nothing), "Fragen" (ask) or "Selbst ausführen" (allow).
// Deleting always asks; reading mail is on or off. The administrator's
// values (Policy.ActionRights, NAS-wide) are the most an account gets;
// every account may be stricter for itself (its own file, OwnActions).
//
// The values never pass through Stricter, ValidMode or Mode: those know
// allow, ask and deny only, and an unknown "suggest" would rank as allow
// there. StricterAction has the order allow < ask < suggest < deny.
//
// 1.12 does not know the field actionRights and drops it when it saves
// the settings; back on 1.13 the defaults below hold again (Q28).

// ModeSuggest: the tool runs as a draft (nothing is written).
const ModeSuggest = "suggest"

// AreaMail is Mail (the e-mail program) for the agent since 1.13. Its
// rights live under actionRights only (mail.read, mail.draft, mail.send,
// mail.move), never under areas or dataRights: 1.12 refuses both.
const AreaMail = "mail"

// The action keys.
const (
	ActMailRead      = "mail.read"
	ActMailDraft     = "mail.draft"
	ActMailSend      = "mail.send"
	ActMailMove      = "mail.move"
	ActEventCreate   = "event.create"
	ActEventUpdate   = "event.update"
	ActEventDelete   = "event.delete"
	ActContactCreate = "contact.create"
	ActContactUpdate = "contact.update"
	ActContactDelete = "contact.delete"
	ActTaskCreate    = "task.create"
	ActTaskUpdate    = "task.update"
	ActTaskComplete  = "task.complete"
	ActTaskDelete    = "task.delete"
)

// ActionKeys in the order the settings show them.
var ActionKeys = []string{ActMailRead, ActMailDraft, ActMailSend, ActMailMove,
	ActEventCreate, ActEventUpdate, ActEventDelete,
	ActContactCreate, ActContactUpdate, ActContactDelete,
	ActTaskCreate, ActTaskUpdate, ActTaskComplete, ActTaskDelete}

// ValidActionKey reports whether key is one of the action keys.
func ValidActionKey(key string) bool { return slices.Contains(ActionKeys, key) }

// ActionArea is the area an action belongs to: reminders are part of the
// calendar (Kalender), as on the iPhone.
func ActionArea(key string) string {
	switch {
	case strings.HasPrefix(key, "mail."):
		return AreaMail
	case strings.HasPrefix(key, "contact."):
		return AreaContacts
	}
	return AreaCalendar
}

// IsDeleteAction reports whether the action deletes something: it always
// asks the person (owner 18 Nov 2026: "Löschen immer mit Bestätigung").
func IsDeleteAction(key string) bool { return strings.HasSuffix(key, ".delete") }

// ValidActionValue reports whether v may be stored for key: mail.read
// allow or deny, a delete ask only, everything else suggest, ask or allow.
func ValidActionValue(key, v string) bool {
	switch {
	case !ValidActionKey(key):
		return false
	case key == ActMailRead:
		return v == ModeAllow || v == ModeDeny
	case IsDeleteAction(key):
		return v == ModeAsk
	}
	return v == ModeSuggest || v == ModeAsk || v == ModeAllow
}

// CheckActions refuses unknown keys and values.
func CheckActions(m map[string]string) error {
	for k, v := range m {
		if !ValidActionKey(k) {
			return errors.New("unknown action " + k)
		}
		if !ValidActionValue(k, v) {
			return errors.New("invalid value for " + k)
		}
	}
	return nil
}

var actionRank = map[string]int{ModeAllow: 0, ModeAsk: 1, ModeSuggest: 2, ModeDeny: 3}

// StricterAction returns the stricter of two action modes; an unknown
// value counts as deny (never as allow).
func StricterAction(a, b string) string {
	ra, ok := actionRank[a]
	if !ok {
		return ModeDeny
	}
	rb, ok := actionRank[b]
	if !ok {
		return ModeDeny
	}
	if rb > ra {
		return b
	}
	return a
}

// mailMode is the gate of the mail area for Mode: reading as mail.read
// says (allow from the factory), writing closed when mail.read is off or
// the quick choice is "Nur lesen"; ActionMode decides the rest.
func (p Policy) mailMode(kind string) string {
	read := ModeAllow
	if v := p.ActionRights[ActMailRead]; v == ModeDeny {
		read = ModeDeny
	}
	if kind == KindRead || read == ModeDeny {
		return read
	}
	if p.Preset == PresetRead {
		return ModeDeny
	}
	return ModeAllow
}

// ActionMode is the mode of an action under the NAS-wide policy (the cap
// the administrator sets), before the account's own choice and the rules
// of the chat. Without an own value (Q28, decided when it is read, never
// written back):
//
//   - mail.read: allow; a delete: ask;
//   - calendar, reminders and contacts with dataRights set: its write axis
//     (whoever chose "Nachfragen" keeps it, "Ohne Nachfrage" stays allow);
//   - quick choice "Alles ohne Nachfrage": allow, but mail.send ask (Q29:
//     sending without asking only when switched on by itself);
//   - "Nur lesen": deny; "Standard" or none: suggest.
//
// A closed area (both data axes or writing denied, mail.read off) is deny
// whatever the key says.
func (p Policy) ActionMode(key string) string {
	if !ValidActionKey(key) {
		return ModeDeny
	}
	area := ActionArea(key)
	if area == AreaMail {
		if key == ActMailRead {
			return p.mailMode(KindRead)
		}
		if p.mailMode(KindChange) == ModeDeny {
			return ModeDeny
		}
	} else if p.DataMode(area, KindChange) == ModeDeny {
		return ModeDeny
	}
	if v := p.ActionRights[key]; ValidActionValue(key, v) {
		return v
	}
	if IsDeleteAction(key) {
		return ModeAsk
	}
	if r, ok := p.DataRights[area]; ok && area != AreaMail && (r.Write == ModeAllow || r.Write == ModeAsk) {
		return r.Write
	}
	switch p.Preset {
	case PresetAll:
		if key == ActMailSend {
			return ModeAsk
		}
		return ModeAllow
	case PresetRead:
		return ModeDeny
	}
	return ModeSuggest
}

// EffectiveAction is the mode that holds for one call: the strictest of
// the NAS-wide value, the account's own (a value it may not store counts
// as nothing) and the rule of the chat – once the chat has read mail,
// events, contacts or other outside content, sending mail always asks
// (Q27; a mail could say "send this to …").
func EffectiveAction(p Policy, own map[string]string, key string, read bool) string {
	mode := p.ActionMode(key)
	if v := own[key]; ValidActionValue(key, v) {
		mode = StricterAction(mode, v)
	}
	// Mail switched off by the account closes all of Mail for it.
	if ActionArea(key) == AreaMail && own[ActMailRead] == ModeDeny {
		mode = ModeDeny
	}
	if IsDeleteAction(key) && mode == ModeAllow {
		mode = ModeAsk
	}
	if key == ActMailSend && read && mode == ModeAllow {
		mode = ModeAsk
	}
	return mode
}

// EffectiveActions are all action modes for an account, for the status
// the pages show (without the rules of a chat).
func EffectiveActions(p Policy, own map[string]string) map[string]string {
	out := make(map[string]string, len(ActionKeys))
	for _, k := range ActionKeys {
		out[k] = EffectiveAction(p, own, k, false)
	}
	return out
}

// ReadsPersonal reports whether a call in an area reads the person's own
// data (calendar with reminders, contacts, mail): the chat is marked
// PIMRead (S-24/S-26), so that writing files and notes, the web and MCP
// and sending mail ask first.
func ReadsPersonal(area string) bool { return IsDataArea(area) || area == AreaMail }
