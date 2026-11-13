package diagnose

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestBundleHoldsTheNVRDiagnosis(t *testing.T) {
	m, _, store := newManager(t, map[string]string{})
	store.Write("users/alex/account.json", []byte(`{"name":"alex"}`))
	store.Write("nvr/cameras.json", []byte(`[{"id":"haustuer","source":{"user":"viewer","password":"kamerageheim"}}]`))
	read := func() map[string]string {
		var buf bytes.Buffer
		if err := m.Bundle(context.Background(), &buf, []string{"alex"}); err != nil {
			t.Fatal(err)
		}
		archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatal(err)
		}
		files := map[string]string{}
		for _, file := range archive.File {
			r, _ := file.Open()
			data, _ := io.ReadAll(r)
			r.Close()
			files[file.Name] = string(data)
		}
		return files
	}
	// The NVR is off (the hook answers nil): no file.
	m.NVR = func(context.Context) any { return nil }
	if _, has := read()["nvr.json"]; has {
		t.Error("nvr.json although the NVR is off")
	}
	m.NVR = func(context.Context) any {
		return map[string]any{"state": "running", "detector": map[string]any{"active": "npu"},
			"log": []string{"frigate.app INFO started", "ffmpeg ERROR rtsp://192.168.1.61:554/sub refused, password=oops"}}
	}
	files := read()
	nvr, has := files["nvr.json"]
	if !has || !strings.Contains(nvr, `"active": "npu"`) || !strings.Contains(nvr, "frigate.app INFO started") {
		t.Fatalf("nvr.json: %q", nvr)
	}
	// Addresses and whatever looks like a secret are taken out like
	// everywhere in the bundle; the cameras' file is never part of it.
	all := ""
	for name, content := range files {
		all += name + "\n" + content
	}
	for _, secret := range []string{"192.168.1.61", "oops", "kamerageheim", "cameras.json"} {
		if strings.Contains(all, secret) {
			t.Errorf("the bundle holds %q", secret)
		}
	}
}
