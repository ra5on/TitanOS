package store

import (
	"bytes"
	"context"
	"filippo.io/age"
	"io"
	"testing"
)

func TestPersonalArchiveRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newEnv(t).store()
	sources := []ArchiveSource{{Calendar, DefaultCalendar}, {Contacts, DefaultBook}, {Tasks, DefaultTaskList}}
	docs := []struct {
		source ArchiveSource
		name   string
		data   []byte
	}{
		{sources[0], "serie.ics", ics("series", "Schicht", "20261111T070000Z", "RRULE:FREQ=WEEKLY;COUNT=3")},
		{sources[1], "a.vcf", vcf("a", "A")},
		{sources[1], "group.vcf", vcf("g", "Familie", "X-ADDRESSBOOKSERVER-KIND:group", "X-ADDRESSBOOKSERVER-MEMBER:urn:uuid:a")},
		{sources[2], "task.ics", vtodo("t", "Warten", "STATUS:COMPLETED", "COMPLETED:20261123T120000Z")},
	}
	for _, d := range docs {
		mustPut(t, s, d.source.Kind, d.source.ID, d.name, d.data)
	}
	if _, err := s.Put(Calendar, "bea", DefaultCalendar, "private.ics", ics("b", "Private", "20261111T070000Z"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	enc, err := age.Encrypt(&buf, key.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.WriteArchive(ctx, enc, "alex", sources); err != nil {
		t.Fatal(err)
	}
	if err = enc.Close(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("Schicht")) {
		t.Fatal("cleartext in backup")
	}
	target := newEnv(t).store()
	read := func(data []byte) io.Reader {
		r, err := age.Decrypt(bytes.NewReader(data), key)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if _, err := target.RestoreArchive(ctx, read(buf.Bytes()[:buf.Len()-1]), "bea", true); err == nil {
		t.Fatal("accepted incomplete encryption")
	}
	report, err := target.RestoreArchive(ctx, read(buf.Bytes()), "bea", false)
	if err != nil || len(report.Collections) != 3 {
		t.Fatalf("probe: %+v %v", report, err)
	}
	books, _ := target.Collections(Contacts, "bea")
	if len(books) != 1 || !books[0].Virtual {
		t.Fatal("probe wrote to live book")
	}
	report, err = target.RestoreArchive(ctx, read(buf.Bytes()), "bea", true)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range report.Collections {
		if c.Added != c.Count {
			t.Fatalf("missing objects: %+v", c)
		}
		for _, d := range docs {
			if d.source != sources[i] {
				continue
			}
			obj, err := target.Get(c.Kind, "bea", c.Target, d.name)
			if err != nil || !bytes.Equal(d.data, obj.Data) {
				t.Fatalf("object %s differs: %v", d.name, err)
			}
		}
	}
	if _, err = target.Put(Contacts, "bea", DefaultBook, "a.vcf", vcf("a", "Edited"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	again, err := target.RestoreArchive(ctx, read(buf.Bytes()), "bea", true)
	if err != nil || again.Collections[1].Conflicts != 1 || again.Collections[1].Identical != 1 {
		t.Fatalf("retry: %+v %v", again, err)
	}
}
