package profile

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// parsePlist reads a property list with encoding/xml into Go values
// (dict → map, array → slice, string, int, bool): a strict check that it
// is well-formed and has the plist structure.
func parsePlist(t *testing.T, data []byte) map[string]any {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	var value func(start xml.StartElement) any
	value = func(start xml.StartElement) any {
		switch start.Name.Local {
		case "dict":
			out := map[string]any{}
			key := ""
			for {
				tok, err := dec.Token()
				if err != nil {
					t.Fatal(err)
				}
				switch tok := tok.(type) {
				case xml.StartElement:
					if tok.Name.Local == "key" {
						var k string
						if err := dec.DecodeElement(&k, &tok); err != nil {
							t.Fatal(err)
						}
						if _, dup := out[k]; dup {
							t.Fatalf("key %s twice", k)
						}
						key = k
						continue
					}
					if key == "" {
						t.Fatalf("value without key: %s", tok.Name.Local)
					}
					out[key] = value(tok)
					key = ""
				case xml.EndElement:
					if key != "" {
						t.Fatalf("key %s without value", key)
					}
					return out
				}
			}
		case "array":
			out := []any{}
			for {
				tok, err := dec.Token()
				if err != nil {
					t.Fatal(err)
				}
				switch tok := tok.(type) {
				case xml.StartElement:
					out = append(out, value(tok))
				case xml.EndElement:
					return out
				}
			}
		case "string":
			var s string
			if err := dec.DecodeElement(&s, &start); err != nil {
				t.Fatal(err)
			}
			return s
		case "integer":
			var n int
			if err := dec.DecodeElement(&n, &start); err != nil {
				t.Fatal(err)
			}
			return n
		case "true", "false":
			if err := dec.Skip(); err != nil {
				t.Fatal(err)
			}
			return start.Name.Local == "true"
		}
		t.Fatalf("unknown element %s", start.Name.Local)
		return nil
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			t.Fatal("no plist")
		}
		if err != nil {
			t.Fatal(err)
		}
		if s, ok := tok.(xml.StartElement); ok && s.Name.Local == "plist" {
			for {
				tok, err := dec.Token()
				if err != nil {
					t.Fatal(err)
				}
				if s, ok := tok.(xml.StartElement); ok {
					m, ok := value(s).(map[string]any)
					if !ok {
						t.Fatal("plist is not a dict")
					}
					return m
				}
			}
		}
	}
}

var testParams = Params{
	Account:  "alex",
	Created:  time.Date(2026, 10, 1, 8, 0, 0, 123, time.UTC),
	Device:   "iPhone von Alex",
	Password: "k7qm-2xrp-9tfl-c4wn-hb3s-m8vd-q2ze",
	Host:     "titan.tail1a2b3.ts.net",
	Port:     443,
	Lang:     "de",
}

func TestBuildHasCalDAVAndCardDAV(t *testing.T) {
	data := Build(testParams)
	p := parsePlist(t, data)
	if p["PayloadType"] != TypeConfiguration || p["PayloadVersion"] != 1 || p["PayloadRemovalDisallowed"] != false {
		t.Fatalf("profile: %v", p)
	}
	content := p["PayloadContent"].([]any)
	if len(content) != 2 {
		t.Fatalf("%d payloads", len(content))
	}
	cal, card := content[0].(map[string]any), content[1].(map[string]any)
	want := map[string]any{
		"PayloadType": TypeCalDAV, "CalDAVHostName": "titan.tail1a2b3.ts.net", "CalDAVPort": 443, "CalDAVUseSSL": true,
		"CalDAVUsername": "alex", "CalDAVPassword": testParams.Password, "CalDAVAccountDescription": "TitanOS Kalender",
		"CalDAVPrincipalURL": "https://titan.tail1a2b3.ts.net/caldav/alex/", "PayloadVersion": 1,
	}
	for k, v := range want {
		if cal[k] != v {
			t.Errorf("CalDAV %s = %v, want %v", k, cal[k], v)
		}
	}
	want = map[string]any{
		"PayloadType": TypeCardDAV, "CardDAVHostName": "titan.tail1a2b3.ts.net", "CardDAVPort": 443, "CardDAVUseSSL": true,
		"CardDAVUsername": "alex", "CardDAVPassword": testParams.Password, "CardDAVAccountDescription": "TitanOS Kontakte",
		"CardDAVPrincipalURL": "https://titan.tail1a2b3.ts.net/carddav/alex/", "PayloadVersion": 1,
	}
	for k, v := range want {
		if card[k] != v {
			t.Errorf("CardDAV %s = %v, want %v", k, card[k], v)
		}
	}
	// No mail account, no other payload.
	if strings.Contains(string(data), "com.apple.mail") || strings.Contains(string(data), "EmailAccount") {
		t.Fatal("a mail account in the profile")
	}
	// Identifiers: each payload its own UUID, below the profile's identifier.
	ids := map[string]bool{p["PayloadUUID"].(string): true}
	for _, c := range content {
		m := c.(map[string]any)
		if !strings.HasPrefix(m["PayloadIdentifier"].(string), p["PayloadIdentifier"].(string)+".") {
			t.Errorf("identifier %v", m["PayloadIdentifier"])
		}
		ids[m["PayloadUUID"].(string)] = true
	}
	if len(ids) != 3 {
		t.Fatalf("UUIDs not distinct: %v", ids)
	}
	for id := range ids {
		if len(id) != 36 || strings.Count(id, "-") != 4 || id[14] != '5' {
			t.Errorf("UUID %s", id)
		}
	}
}

// The same device made again (another password) gets the same identifiers,
// so iOS replaces the profile; another device, another account of the same
// name or another case of the name does what it should.
func TestIdentifiersStablePerDevice(t *testing.T) {
	ident := func(p Params) string {
		m := parsePlist(t, Build(p))
		return m["PayloadIdentifier"].(string) + " " + m["PayloadUUID"].(string)
	}
	base := ident(testParams)
	again := testParams
	again.Password, again.Host, again.Lang = "other", "192.168.1.20", "en"
	if ident(again) != base {
		t.Fatal("same device: identifiers changed")
	}
	upper := testParams
	upper.Device = "IPHONE VON ALEX "
	if ident(upper) != base {
		t.Fatal("case of the name changed the identifiers")
	}
	other := testParams
	other.Device = "iPad"
	if ident(other) == base {
		t.Fatal("another device, same identifiers")
	}
	renewed := testParams
	renewed.Created = renewed.Created.Add(time.Second)
	if ident(renewed) == base {
		t.Fatal("new account of the name, same identifiers")
	}
}

func TestBuildEscapesAndPorts(t *testing.T) {
	p := testParams
	p.Account, p.Device, p.Host, p.Port, p.Lang = "a&b", `<iPhone> "&" ]]>`, "fd7a:115c::1", 8443, "en"
	p.Holidays = &Subscription{URL: "https://[fd7a:115c::1]:8443/api/v1/holidays/de-by.ics?lang=en&x=1", Description: "Holidays (Bavaria)"}
	data := Build(p)
	m := parsePlist(t, data)
	content := m["PayloadContent"].([]any)
	if len(content) != 3 {
		t.Fatalf("%d payloads", len(content))
	}
	cal := content[0].(map[string]any)
	if cal["CalDAVUsername"] != "a&b" || cal["CalDAVPort"] != 8443 || cal["CalDAVPrincipalURL"] != "https://[fd7a:115c::1]:8443/caldav/a&b/" {
		t.Fatalf("caldav %v", cal)
	}
	if !strings.Contains(m["PayloadDescription"].(string), `<iPhone> "&" ]]>`) || !strings.Contains(m["PayloadDescription"].(string), "Holds the device password") {
		t.Fatalf("description %q", m["PayloadDescription"])
	}
	sub := content[2].(map[string]any)
	if sub["PayloadType"] != TypeSubscribed || sub["SubCalAccountHostName"] != p.Holidays.URL || sub["SubCalAccountUseSSL"] != true || sub["SubCalAccountDescription"] != "Holidays (Bavaria)" {
		t.Fatalf("subscription %v", sub)
	}
	if _, ok := sub["SubCalAccountPassword"]; ok {
		t.Fatal("password for the holidays")
	}
	pythonPlist(t, data)
}

// pythonPlist reads the profile with Python's plistlib as well, when
// Python is there (a second, independent parser).
func pythonPlist(t *testing.T, data []byte) {
	t.Helper()
	bin, err := exec.LookPath("python3")
	if err != nil {
		t.Log("python3 not installed: checked with encoding/xml only")
		return
	}
	cmd := exec.Command(bin, "-I", "-c", "import json,plistlib,sys; print(json.dumps(plistlib.loads(sys.stdin.buffer.read())))")
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plistlib: %v\n%s", err, out)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil || m["PayloadType"] != TypeConfiguration {
		t.Fatalf("plistlib read %s", out)
	}
}

func TestSignedProfileVerifies(t *testing.T) {
	_, cert := testChain(t, "ecdsa")
	data := Build(testParams)
	signed, err := Sign(data, &cert, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := Verify(signed)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("verify: %v", err)
	}
}
