package card

import (
	"bytes"
	"strings"
	"testing"
)

// A card read and written without changes keeps its bytes (CRLF); a
// touched line is written anew and reads back the same.
func TestDocRoundTrip(t *testing.T) {
	in := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nitem1.EMAIL;type=INTERNET;X-X=\"a:b,c\":a@b\r\nNOTE:lang\r\n er Text\r\nFN:A\r\nEND:VCARD\r\n")
	d, err := ParseDoc(in)
	if err != nil || !bytes.Equal(d.Bytes(), in) {
		t.Fatalf("%v\n%q", err, d.Bytes())
	}
	e := d.First("EMAIL")
	if e.Group != "item1" || !slices2(e.Param("X-X"), "a:b,c") || d.First("NOTE").Value != "langer Text" {
		t.Fatalf("%+v", e)
	}
	e.Touch()
	again, err := ParseDoc(d.Bytes())
	if err != nil || !slices2(again.First("EMAIL").Param("X-X"), "a:b,c") || again.First("EMAIL").Value != "a@b" {
		t.Fatalf("%v %q", err, d.Bytes())
	}
	if got := Structured(`a\;b;c\,d\\;e\nf`); len(got) != 3 || got[0] != "a;b" || got[1] != `c,d\` || got[2] != "e\nf" {
		t.Fatalf("%q", got)
	}
	if JoinStructured([]string{"a;b", "c,d"}) != `a\;b;c\,d` {
		t.Fatal("join")
	}
}

func slices2(v []string, want ...string) bool {
	return strings.Join(v, "\x00") == strings.Join(want, "\x00")
}

// FuzzDoc: no input panics; what reads writes back to something that
// reads with the same lines.
func FuzzDoc(f *testing.F) {
	f.Add([]byte("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nEND:VCARD\r\n"))
	f.Add([]byte("BEGIN:VCARD\nVERSION:4.0\nitem1.TEL;TYPE=\"a,b\":1\n item1.X-ABLabel:x\nPHOTO:data:image/jpeg;base64,/9j/\nEND:VCARD"))
	f.Add([]byte("BEGIN:VCARD\r\nPHOTO;ENCODING=b:\r\n AAAA\r\nEND:VCARD\r\n"))
	f.Add([]byte(":\r\n\"\r\n;;;\r\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		d, err := ParseDoc(b)
		if err != nil {
			return
		}
		_, _ = d.PhotoData()
		for _, l := range d.Lines {
			l.Touch()
			_ = Structured(l.Value)
		}
		again, err := ParseDoc(d.Bytes())
		if err != nil || len(again.Lines) != len(d.Lines) {
			t.Fatalf("written %q: %v", d.Bytes(), err)
		}
	})
}
