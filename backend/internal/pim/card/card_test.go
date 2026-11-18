package card

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-vcard"
)

// iosCard is shaped like a contact the iOS Contacts app sends over CardDAV.
const iosCard = "BEGIN:VCARD\r\n" +
	"VERSION:3.0\r\n" +
	"PRODID:-//Apple Inc.//iPhone OS 26.1//EN\r\n" +
	"N:Muster;Erika;;;\r\n" +
	"FN:Erika Muster\r\n" +
	"ORG:Beispiel GmbH;\r\n" +
	"item1.EMAIL;type=INTERNET;type=pref:erika@example.org\r\n" +
	"item1.X-ABLabel:_$!<Other>!$_\r\n" +
	"TEL;type=CELL;type=VOICE;type=pref:+49 151 00000000\r\n" +
	"BDAY;VALUE=date:1980-05-17\r\n" +
	"UID:6C7B1D2E-0000-4000-8000-000000000001\r\n" +
	"END:VCARD\r\n"

func TestParseIOSCard(t *testing.T) {
	c, err := Parse([]byte(iosCard))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.PreferredValue(vcard.FieldFormattedName); got != "Erika Muster" {
		t.Fatalf("FN = %q", got)
	}
	if got := c.Value(vcard.FieldEmail); got != "erika@example.org" {
		t.Fatalf("EMAIL = %q", got)
	}
}

func TestParseRejects(t *testing.T) {
	two := iosCard + iosCard
	cases := map[string]string{
		"empty":      "",
		"two cards":  two,
		"no version": strings.Replace(iosCard, "VERSION:3.0\r\n", "", 1),
		"version 2":  strings.Replace(iosCard, "VERSION:3.0", "VERSION:2.1", 1),
		"no FN":      strings.Replace(iosCard, "FN:Erika Muster\r\n", "", 1),
		"not vcard":  "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n",
	}
	for name, in := range cases {
		if _, err := Parse([]byte(in)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	big := bytes.Repeat([]byte("x"), MaxSize+1)
	if _, err := Parse(big); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: err = %v", err)
	}
}
