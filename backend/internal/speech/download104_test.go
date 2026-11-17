package speech

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Cancel and download the same model again at once, while the cancelled
// download is still busy where it does not look at its cancellation (the
// sync of a large file): the old download neither installs its files nor
// removes the folder or the entry of the new one; the new one starts once
// the old one is out of the way and installs the model.
func TestCancelAndDownloadAgainKeepsTheNewDownload(t *testing.T) {
	m := testManager(t, fakeHub(t, "the-model"))
	if err := m.SetSettings(Settings{Volume: "Data"}); err != nil {
		t.Fatal(err)
	}
	reached := make(chan int, 2)
	release := []chan struct{}{make(chan struct{}), make(chan struct{})}
	calls := 0
	m.afterFiles = func() {
		calls++
		n := calls
		reached <- n
		<-release[n-1]
	}
	wait := func(want int) {
		t.Helper()
		select {
		case n := <-reached:
			if n != want {
				t.Fatalf("download %d reached its end, want %d", n, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("download %d did not reach its end", want)
		}
	}
	if err := m.Download(context.Background(), "model", "tiny"); err != nil {
		t.Fatal(err)
	}
	wait(1)
	m.Cancel("model", "tiny")
	if err := m.Download(context.Background(), "model", "tiny"); err != nil {
		t.Fatal(err)
	}
	close(release[0]) // the cancelled download goes on
	wait(2)
	if _, ok := m.Jobs()["model/tiny"]; !ok {
		t.Fatal("the cancelled download removed the entry of the new one")
	}
	close(release[1])
	if jobs := waitJobs(t, m); len(jobs) != 0 {
		t.Fatalf("jobs: %+v", jobs)
	}
	if data, err := os.ReadFile(m.hostPath(whisperDir, "tiny", "model.bin")); err != nil || string(data) != "the-model" {
		t.Fatalf("model not installed by the new download: %q %v", data, err)
	}
}

// A download whose data stop coming fails after stallLimit instead of
// staying "running" for good.
func TestStalledDownloadFails(t *testing.T) {
	limit := stallLimit
	stallLimit = 100 * time.Millisecond
	t.Cleanup(func() { stallLimit = limit })
	h := fakeHub(t, "the-model")
	stuck := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/model.bin") {
			w.Header().Set("Content-Length", "9")
			w.Write([]byte("the"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		res, err := h.client.Get(h.base + r.URL.Path + "?" + r.URL.RawQuery)
		if err != nil {
			t.Error(err)
			return
		}
		defer res.Body.Close()
		w.WriteHeader(res.StatusCode)
		buf := make([]byte, 1<<16)
		n, _ := res.Body.Read(buf)
		w.Write(buf[:n])
	}))
	t.Cleanup(stuck.Close)
	m := testManager(t, &hub{base: stuck.URL, client: stuck.Client()})
	if err := m.SetSettings(Settings{Volume: "Data"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Download(context.Background(), "model", "tiny"); err != nil {
		t.Fatal(err)
	}
	jobs := waitJobs(t, m)
	if j, ok := jobs["model/tiny"]; !ok || !strings.Contains(j.Error, "no data") {
		t.Fatalf("jobs: %+v", jobs)
	}
}
