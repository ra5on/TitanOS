// Package profile makes the configuration profile (.mobileconfig) that sets
// up an iPhone for T-Kalender and Kontakte in one installation (1.13,
// decision of the owner of 18 Nov 2026): a CalDAV account (calendars), a
// CardDAV account (contacts), optionally a subscribed calendar of public
// holidays; never a mail account. The payload keys are those of Apple's
// Device Management reference (CalDAV, CardDAV, SubscribedCalendar).
//
// The profile carries a device password and is a secret: it is made only
// for one fetch (Links) or one download in the web session, never kept.
// Sign wraps it into CMS SignedData with the certificate of the web UI when
// that one is trusted by iOS (Let's Encrypt, Tailscale).
package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Payload types (Apple Device Management).
const (
	TypeConfiguration = "Configuration"
	TypeCalDAV        = "com.apple.caldav.account"
	TypeCardDAV       = "com.apple.carddav.account"
	TypeSubscribed    = "com.apple.subscribedcalendar.account"
)

// ContentType is the media type iOS installs a profile from.
const ContentType = "application/x-apple-aspen-config"

// Subscription is a calendar the iPhone subscribes to (the holidays).
type Subscription struct {
	URL         string // https://…/api/v1/holidays/de-by.ics
	Description string // „Feiertage (Bayern)“
}

// Params are what one profile is made of.
type Params struct {
	Account string
	// Created is the account's creation time: with the account and the
	// device name it makes the identifiers stable (Identity).
	Created  time.Time
	Device   string
	Password string
	// Host is a name or address without port; Port the port of the web UI.
	Host string
	Port int
	// Lang is "de" or "en" (the texts iOS shows while installing).
	Lang     string
	Holidays *Subscription
}

// texts are what iOS shows of the profile, in German and English.
var texts = map[string]map[string]string{
	"de": {
		"name":        "TitanOS – Kalender und Kontakte",
		"description": "Kalender und Kontakte des Kontos „%s“ auf %s für das Gerät „%s“. Enthält das Gerätepasswort dieses Geräts; entziehen kannst du es in TitanOS unter „Am iPhone einrichten“.",
		"caldav":      "TitanOS Kalender",
		"carddav":     "TitanOS Kontakte",
		"caldavName":  "Kalender (CalDAV)",
		"carddavName": "Kontakte (CardDAV)",
		"subName":     "Feiertage (Abonnement)",
	},
	"en": {
		"name":        "TitanOS – Calendar and Contacts",
		"description": "Calendars and contacts of the account “%s” on %s for the device “%s”. Holds the device password of this device; revoke it in TitanOS under “Set up on iPhone”.",
		"caldav":      "TitanOS Calendar",
		"carddav":     "TitanOS Contacts",
		"caldavName":  "Calendar (CalDAV)",
		"carddavName": "Contacts (CardDAV)",
		"subName":     "Holidays (subscription)",
	},
}

func text(lang, key string) string {
	if t, ok := texts[lang]; ok {
		return t[key]
	}
	return texts["de"][key]
}

// identity is the hash the identifiers come from: the account (with its
// creation time, so a new account of the same name gets new ones) and the
// device name without regard to case. The same device made again under
// its name gets the same identifiers, and iOS replaces the installed
// profile instead of adding a second one.
func identity(p Params) [32]byte {
	return sha256.Sum256([]byte("titanos-profile\x00" + p.Account + "\x00" + strconv.FormatInt(p.Created.UnixNano(), 10) + "\x00" + strings.ToLower(strings.TrimSpace(p.Device))))
}

// Identifier is the PayloadIdentifier of the profile of a device.
func Identifier(p Params) string {
	id := identity(p)
	return "titanos.pim." + hex.EncodeToString(id[:8])
}

// uuid is a name-based UUID (version 5 layout, SHA-256 instead of SHA-1)
// of the profile of a device and one of its parts.
func uuid(p Params, part string) string {
	id := identity(p)
	sum := sha256.Sum256(append(id[:], []byte("\x00"+part)...))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x50
	b[8] = b[8]&0x3f | 0x80
	h := strings.ToUpper(hex.EncodeToString(b))
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// plist writes the XML of a property list; every string escaped.
type plist struct{ b strings.Builder }

func (w *plist) raw(s string) { w.b.WriteString(s) }

func (w *plist) str(key, value string) {
	w.raw("<key>")
	xml.EscapeText(&w.b, []byte(key))
	w.raw("</key><string>")
	xml.EscapeText(&w.b, []byte(value))
	w.raw("</string>\n")
}

func (w *plist) integer(key string, value int) {
	w.raw("<key>" + key + "</key><integer>" + strconv.Itoa(value) + "</integer>\n")
}

func (w *plist) boolean(key string, value bool) {
	if value {
		w.raw("<key>" + key + "</key><true/>\n")
	} else {
		w.raw("<key>" + key + "</key><false/>\n")
	}
}

// hostPort is the host for a URL: IPv6 in brackets, the port unless 443.
func hostPort(host string, port int) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != 443 {
		host += ":" + strconv.Itoa(port)
	}
	return host
}

// PrincipalURL is the address of the account's principal in davsync.
func PrincipalURL(kind, host string, port int, account string) string {
	return "https://" + hostPort(host, port) + "/" + kind + "/" + url.PathEscape(account) + "/"
}

// Build returns the profile as an XML property list (unsigned).
func Build(p Params) []byte {
	if p.Port <= 0 {
		p.Port = 443
	}
	top := Identifier(p)
	w := &plist{}
	w.raw(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	w.raw(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	w.raw(`<plist version="1.0">` + "\n<dict>\n<key>PayloadContent</key>\n<array>\n")

	w.raw("<dict>\n")
	w.str("CalDAVAccountDescription", text(p.Lang, "caldav"))
	w.str("CalDAVHostName", p.Host)
	w.integer("CalDAVPort", p.Port)
	w.str("CalDAVPrincipalURL", PrincipalURL("caldav", p.Host, p.Port, p.Account))
	w.boolean("CalDAVUseSSL", true)
	w.str("CalDAVUsername", p.Account)
	w.str("CalDAVPassword", p.Password)
	w.str("PayloadDisplayName", text(p.Lang, "caldavName"))
	w.str("PayloadIdentifier", top+".caldav")
	w.str("PayloadType", TypeCalDAV)
	w.str("PayloadUUID", uuid(p, "caldav"))
	w.integer("PayloadVersion", 1)
	w.raw("</dict>\n")

	w.raw("<dict>\n")
	w.str("CardDAVAccountDescription", text(p.Lang, "carddav"))
	w.str("CardDAVHostName", p.Host)
	w.integer("CardDAVPort", p.Port)
	w.str("CardDAVPrincipalURL", PrincipalURL("carddav", p.Host, p.Port, p.Account))
	w.boolean("CardDAVUseSSL", true)
	w.str("CardDAVUsername", p.Account)
	w.str("CardDAVPassword", p.Password)
	w.str("PayloadDisplayName", text(p.Lang, "carddavName"))
	w.str("PayloadIdentifier", top+".carddav")
	w.str("PayloadType", TypeCardDAV)
	w.str("PayloadUUID", uuid(p, "carddav"))
	w.integer("PayloadVersion", 1)
	w.raw("</dict>\n")

	if p.Holidays != nil {
		w.raw("<dict>\n")
		w.str("SubCalAccountDescription", p.Holidays.Description)
		w.str("SubCalAccountHostName", p.Holidays.URL)
		w.boolean("SubCalAccountUseSSL", strings.HasPrefix(p.Holidays.URL, "https://"))
		w.str("PayloadDisplayName", text(p.Lang, "subName"))
		w.str("PayloadIdentifier", top+".holidays")
		w.str("PayloadType", TypeSubscribed)
		w.str("PayloadUUID", uuid(p, "holidays"))
		w.integer("PayloadVersion", 1)
		w.raw("</dict>\n")
	}

	w.raw("</array>\n")
	w.str("PayloadDescription", fmt.Sprintf(text(p.Lang, "description"), p.Account, p.Host, strings.TrimSpace(p.Device)))
	w.str("PayloadDisplayName", text(p.Lang, "name"))
	w.str("PayloadIdentifier", top)
	w.str("PayloadOrganization", "TitanOS")
	w.boolean("PayloadRemovalDisallowed", false)
	w.str("PayloadType", TypeConfiguration)
	w.str("PayloadUUID", uuid(p, "profile"))
	w.integer("PayloadVersion", 1)
	w.raw("</dict>\n</plist>\n")
	return []byte(w.b.String())
}

// FileName is the name of the download: ASCII only.
const FileName = "TitanOS.mobileconfig"
