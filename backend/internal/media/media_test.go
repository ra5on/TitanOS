package media

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

func testISO(size int) []byte {
	if size < 40<<10 {
		size = 40 << 10
	}
	data := make([]byte, size)
	copy(data[16*2048+1:], "CD001")
	for i := 20 * 2048; i < len(data); i++ {
		data[i] = byte(i % 251)
	}
	return data
}
func testManager(t *testing.T) *Manager {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "data"), 0755)
	m := New(context.Background(), func() []string { return []string{"data"} }, os.Getgid())
	m.MountRoot = root
	t.Cleanup(m.Close)
	return m
}
func waitJob(t *testing.T, m *Manager, id string, phases ...string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range m.List() {
			if j.ID == id && slicesContains(phases, j.Phase) {
				return j
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job did not reach %v: %+v", phases, m.List())
	return Job{}
}
func slicesContains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func TestGzipDownloadHashAndPublication(t *testing.T) {
	m := testManager(t)
	image := testISO(80 << 10)
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	w.Write(image)
	w.Close()
	sum := sha256.Sum256(compressed.Bytes())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"fixture"`)
		w.Write(compressed.Bytes())
	}))
	defer server.Close()
	j, err := m.Create(Request{Volume: "data", Name: "linux.iso", URL: server.URL + "/linux.iso.gz", SHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	j = waitJob(t, m, j.ID, "done", "failed", "review")
	if j.Phase != "done" {
		t.Fatal(j)
	}
	data, err := os.ReadFile(j.Output)
	if err != nil || !bytes.Equal(data, image) {
		t.Fatal("wrong published image", err)
	}
	if _, err := os.Stat(filepath.Join(m.MountRoot, "data/.titanos/media", j.ID, "source.part")); !os.IsNotExist(err) {
		t.Fatal("source archive retained")
	}
	if err := m.Control(j.ID, "remove"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(j.Output); err != nil {
		t.Fatal("removing a job removed published data")
	}
}
func TestZIPUploadRejectsAmbiguityAndNeverOverwrites(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    []string
		existing bool
		ok       bool
	}{{"one", []string{"disk.iso"}, false, true}, {"many", []string{"one.iso", "two.qcow2"}, false, false}, {"traversal", []string{"../disk.iso"}, false, false}, {"exists", []string{"disk.iso"}, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			m := testManager(t)
			var buf bytes.Buffer
			archive := zip.NewWriter(&buf)
			for _, name := range tc.files {
				w, _ := archive.Create(name)
				w.Write(testISO(40 << 10))
			}
			archive.Close()
			if tc.existing {
				os.MkdirAll(filepath.Join(m.MountRoot, "data/iso"), 0755)
				os.WriteFile(filepath.Join(m.MountRoot, "data/iso/disk.iso"), []byte("keep"), 0640)
			}
			j, err := m.Create(Request{Volume: "data", Name: "disk.iso", UploadName: "image.zip", UploadSize: int64(buf.Len())})
			if err != nil {
				t.Fatal(err)
			}
			if err = m.Upload(context.Background(), j.ID, 0, buf.Bytes()); err != nil {
				t.Fatal(err)
			}
			if err = m.FinishUpload(j.ID); err != nil {
				t.Fatal(err)
			}
			j = waitJob(t, m, j.ID, "done", "failed", "review")
			if (j.Phase == "done") != tc.ok {
				t.Fatal(j)
			}
			if tc.existing {
				data, _ := os.ReadFile(filepath.Join(m.MountRoot, "data/iso/disk.iso"))
				if string(data) != "keep" {
					t.Fatal("existing file overwritten")
				}
			}
		})
	}
}
func TestDownloadPauseRestartResumeUsesHTTPValidator(t *testing.T) {
	m := testManager(t)
	image := testISO(2 << 20)
	var resumed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := int64(0)
		if value := r.Header.Get("Range"); value != "" {
			fmt.Sscanf(value, "bytes=%d-", &start)
			if r.Header.Get("If-Range") != `"fixed"` {
				t.Error("missing range validator")
			}
			resumed.Store(true)
		}
		w.Header().Set("ETag", `"fixed"`)
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(image))-start, 10))
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(image)-1, len(image)))
			w.WriteHeader(206)
		}
		for at := start; at < int64(len(image)); at += 32 << 10 {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			end := min(at+(32<<10), int64(len(image)))
			if _, err := w.Write(image[at:end]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			time.Sleep(3 * time.Millisecond)
		}
	}))
	defer server.Close()
	j, err := m.Create(Request{Volume: "data", Name: "resume.iso", URL: server.URL + "/resume.iso"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		e, _ := m.get(j.ID)
		if m.snapshot(e).Bytes > 64<<10 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err = m.Control(j.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	waitJob(t, m, j.ID, "paused")
	m.Close()
	restarted := New(context.Background(), m.Mounted, os.Getgid())
	restarted.MountRoot = m.MountRoot
	defer restarted.Close()
	if err = restarted.Control(j.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	j = waitJob(t, restarted, j.ID, "done", "failed", "review")
	if j.Phase != "done" || !resumed.Load() {
		t.Fatal(j, resumed.Load())
	}
	data, _ := os.ReadFile(j.Output)
	if !bytes.Equal(data, image) {
		t.Fatal("resumed data differs")
	}
}
func TestCorruptAndUnsafeStateNeverPublishes(t *testing.T) {
	m := testManager(t)
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(m.MountRoot, "data/.titanos"))
	if _, err := m.Create(Request{Volume: "data", Name: "disk.iso", UploadName: "disk.iso", UploadSize: 40960}); err == nil {
		t.Fatal("symlinked private state accepted")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("outside root modified")
	}
	os.Remove(filepath.Join(m.MountRoot, "data/.titanos"))
	data := testISO(40 << 10)
	j, err := m.Create(Request{Volume: "data", Name: "bad.iso", UploadName: "bad.iso", UploadSize: int64(len(data)), SHA256: strings.Repeat("0", 64)})
	if err != nil {
		t.Fatal(err)
	}
	m.Upload(context.Background(), j.ID, 0, data)
	m.FinishUpload(j.ID)
	j = waitJob(t, m, j.ID, "failed")
	if !strings.Contains(j.Error, "SHA-256") {
		t.Fatal(j)
	}
	if _, err := os.Stat(filepath.Join(m.MountRoot, "data/iso/bad.iso")); !os.IsNotExist(err) {
		t.Fatal("bad hash was published")
	}
}
func TestInterruptedPublicationRequiresReview(t *testing.T) {
	m := testManager(t)
	j, err := m.Create(Request{Volume: "data", Name: "disk.iso", UploadName: "disk.iso", UploadSize: 40960})
	if err != nil {
		t.Fatal(err)
	}
	root, err := m.private("data")
	if err != nil {
		t.Fatal(err)
	}
	jobRoot, err := secureJob(root, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	j.Phase = "publishing"
	if err = volumedata.WriteJSON(jobRoot, "job.json", j); err != nil {
		t.Fatal(err)
	}
	jobRoot.Close()
	root.Close()
	restarted := New(context.Background(), m.Mounted, os.Getgid())
	restarted.MountRoot = m.MountRoot
	defer restarted.Close()
	job := waitJob(t, restarted, j.ID, "review")
	if job.Phase != "review" {
		t.Fatal(job)
	}
	if err = restarted.Control(j.ID, "resume"); err == nil {
		t.Fatal("unknown publication repeated")
	}
}

func TestUploadResumeVerifiesEveryExistingByte(t *testing.T) {
	m := testManager(t)
	data := testISO(80 << 10)
	j, err := m.Create(Request{Volume: "data", Name: "resume.iso", UploadName: "resume.iso", UploadSize: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	prefix := data[:40<<10]
	if err = m.Upload(context.Background(), j.ID, 0, prefix); err != nil {
		t.Fatal(err)
	}
	m.Close()
	resumed := New(context.Background(), m.Mounted, os.Getgid())
	resumed.MountRoot = m.MountRoot
	defer resumed.Close()
	if err = resumed.Upload(context.Background(), j.ID, int64(len(prefix)), data[len(prefix):]); err == nil {
		t.Fatal("unverified prefix resumed")
	}
	proof := UploadProof{Offset: 0, Size: int64(len(prefix)), SHA256: strings.Repeat("0", 64)}
	if err = resumed.VerifyUpload(context.Background(), j.ID, []UploadProof{proof}); err == nil {
		t.Fatal("different source accepted")
	}
	hash := sha256.Sum256(prefix)
	proof.SHA256 = hex.EncodeToString(hash[:])
	if err = resumed.VerifyUpload(context.Background(), j.ID, []UploadProof{proof}); err != nil {
		t.Fatal(err)
	}
	if err = resumed.Upload(context.Background(), j.ID, int64(len(prefix)), data[len(prefix):]); err != nil {
		t.Fatal(err)
	}
	if err = resumed.FinishUpload(j.ID); err != nil {
		t.Fatal(err)
	}
	result := waitJob(t, resumed, j.ID, "done", "failed")
	if result.Phase != "done" {
		t.Fatal(result)
	}
}

func TestXZAndPublicationReview(t *testing.T) {
	m := testManager(t)
	image := testISO(80 << 10)
	cmd := exec.Command("xz", "-c")
	cmd.Stdin = bytes.NewReader(image)
	compressed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	j, err := m.Create(Request{Volume: "data", Name: "xz.iso", UploadName: "xz.iso.xz", UploadSize: int64(len(compressed))})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Upload(context.Background(), j.ID, 0, compressed); err != nil {
		t.Fatal(err)
	}
	if err = m.FinishUpload(j.ID); err != nil {
		t.Fatal(err)
	}
	j = waitJob(t, m, j.ID, "done", "failed")
	if j.Phase != "done" {
		t.Fatal(j)
	}
	e, _ := m.get(j.ID)
	m.update(e, func(j *Job) { j.Phase = "review"; j.Output = "" })
	if err = m.Control(j.ID, "review"); err != nil {
		t.Fatal(err)
	}
	if m.snapshot(e).Phase != "done" {
		t.Fatal(m.snapshot(e))
	}
	m.update(e, func(j *Job) { j.Phase = "review" })
	os.WriteFile(j.Output, []byte("replaced"), 0640)
	if err = m.Control(j.ID, "review"); err == nil {
		t.Fatal("different output accepted")
	}
	if m.snapshot(e).Phase != "review" {
		t.Fatal("failed verification cleared review")
	}
	m.Mounted = func() []string { return nil }
	if err = m.verifyPublished(j); err == nil {
		t.Fatal("unmounted output accepted")
	}
}

func TestUploadPauseCannotBeUndoneByTheNextChunk(t *testing.T) {
	m := testManager(t)
	data := testISO(80 << 10)
	j, err := m.Create(Request{Volume: "data", Name: "paused.iso", UploadName: "paused.iso", UploadSize: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	prefix := data[:40<<10]
	if err = m.Upload(context.Background(), j.ID, 0, prefix); err != nil {
		t.Fatal(err)
	}
	if err = m.Control(j.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	if err = m.Upload(context.Background(), j.ID, int64(len(prefix)), data[len(prefix):]); err == nil {
		t.Fatal("next chunk undid server pause")
	}
	if err = m.Control(j.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	if err = m.Upload(context.Background(), j.ID, int64(len(prefix)), data[len(prefix):]); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPResumeRejectsChangedValidatorAndResetsOnFullResponse(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(strconv.FormatBool(full), func(t *testing.T) {
			m := testManager(t)
			data := testISO(80 << 10)
			j, err := m.Create(Request{Volume: "data", Name: "validator.iso", UploadName: "validator.iso", UploadSize: int64(len(data))})
			if err != nil {
				t.Fatal(err)
			}
			prefix := data[:40<<10]
			if err = m.Upload(context.Background(), j.ID, 0, prefix); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Range") != "bytes=40960-" {
					t.Error("missing range", r.Header)
				}
				w.Header().Set("ETag", `"new"`)
				if full {
					w.Write(data)
					return
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", len(prefix), len(data)-1, len(data)))
				w.WriteHeader(206)
				w.Write(data[len(prefix):])
			}))
			defer server.Close()
			e, _ := m.get(j.ID)
			m.update(e, func(j *Job) { j.URL = server.URL; j.ETag = `"old"` })
			if err = m.start(e); err != nil {
				t.Fatal(err)
			}
			j = waitJob(t, m, j.ID, "done", "failed")
			if (j.Phase == "done") != full {
				t.Fatal(j)
			}
			if full {
				out, _ := os.ReadFile(j.Output)
				if !bytes.Equal(out, data) {
					t.Fatal("mixed HTTP source")
				}
			}
		})
	}
}

func TestUploadProofDiscardsOnlyUncheckpointedTail(t *testing.T) {
	m := testManager(t)
	data := testISO(80 << 10)
	j, err := m.Create(Request{Volume: "data", Name: "tail.iso", UploadName: "tail.iso", UploadSize: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	prefix := data[:40<<10]
	if err = m.Upload(context.Background(), j.ID, 0, prefix); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.MountRoot, "data/.titanos/media", j.ID, "source.part")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("uncommitted"))
	f.Close()
	sum := sha256.Sum256(prefix)
	if err = m.VerifyUpload(context.Background(), j.ID, []UploadProof{{Offset: 0, Size: int64(len(prefix)), SHA256: hex.EncodeToString(sum[:])}}); err != nil {
		t.Fatal(err)
	}
	stat, _ := os.Stat(path)
	if stat.Size() != int64(len(prefix)) {
		t.Fatal(stat.Size())
	}
}

type officialTransport struct {
	base     http.RoundTripper
	target   *url.URL
	insecure atomic.Bool
}

func (t *officialTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "fedorapeople.org" {
		t.insecure.Store(true)
		return nil, fmt.Errorf("insecure or foreign request")
	}
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = t.target.Scheme
	u.Host = t.target.Host
	copy.URL = &u
	return t.base.RoundTrip(copy)
}
func TestOfficialAliasUpgradesSameOriginRedirectsBeforeTransport(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(strconv.FormatBool(foreign), func(t *testing.T) {
			m := testManager(t)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "stable-virtio") {
					target := "http://fedorapeople.org/groups/virt/virtio-win/direct-downloads/archive-virtio/version/virtio.iso"
					if foreign {
						target = "https://untrusted.example/virtio.iso"
					}
					http.Redirect(w, r, target, http.StatusMovedPermanently)
					return
				}
				w.Write(testISO(80 << 10))
			}))
			defer server.Close()
			target, _ := url.Parse(server.URL)
			tr := &officialTransport{base: server.Client().Transport, target: target}
			m.Client.Transport = tr
			j, err := m.VirtIO("data")
			if err != nil {
				t.Fatal(err)
			}
			j = waitJob(t, m, j.ID, "done", "failed")
			if (j.Phase == "done") == foreign || tr.insecure.Load() {
				t.Fatal(j, tr.insecure.Load())
			}
		})
	}
}

func TestTerminalPhaseIsNotPublicBeforeWorkerReleasesStaging(t *testing.T) {
	m := testManager(t)
	id := "00112233445566778899aabb"
	m.jobs[id] = &entry{job: Job{ID: id, Volume: "data", Name: "image.iso", Phase: "done"}, running: true}
	if got := m.List()[0].Phase; got != "publishing" {
		t.Fatal(got)
	}
	m.jobs[id].running = false
	if got := m.List()[0].Phase; got != "done" {
		t.Fatal(got)
	}
}

func uploadAndWait(t *testing.T, m *Manager, name, upload string, data []byte) Job {
	t.Helper()
	j, err := m.Create(Request{Volume: "data", Name: name, UploadName: upload, UploadSize: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Upload(context.Background(), j.ID, 0, data); err != nil {
		t.Fatal(err)
	}
	if err = m.FinishUpload(j.ID); err != nil {
		t.Fatal(err)
	}
	return waitJob(t, m, j.ID, "done", "failed", "review")
}

func TestXZMemoryLimitRejectsLargeDictionary(t *testing.T) {
	// The default preset uses an 8 MiB dictionary: with a 1 MiB limit the
	// decoder must refuse instead of allocating, as for a crafted archive
	// above the real 256 MiB limit.
	old := xzMemLimit
	xzMemLimit = "1MiB"
	t.Cleanup(func() { xzMemLimit = old })
	cmd := exec.Command("xz", "-6", "-c")
	cmd.Stdin = bytes.NewReader(testISO(80 << 10))
	compressed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	m := testManager(t)
	j := uploadAndWait(t, m, "big.iso", "big.iso.xz", compressed)
	if j.Phase != "failed" || !strings.Contains(j.Error, "needs more than 1MiB of memory") {
		t.Fatal(j)
	}
	if _, err := os.Stat(filepath.Join(m.MountRoot, "data/iso/big.iso")); err == nil {
		t.Fatal("published")
	}
}

func TestZIPDirectoryIsBounded(t *testing.T) {
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	w, _ := archive.Create("disk.iso")
	w.Write(testISO(40 << 10))
	archive.Close()
	// The end record claims a huge directory: archive/zip would preallocate
	// for it before reading anything.
	crafted := bytes.Clone(buf.Bytes())
	end := bytes.LastIndex(crafted, []byte{0x50, 0x4b, 0x05, 0x06})
	copy(crafted[end+8:], []byte{0xfe, 0xff, 0xfe, 0xff})
	m := testManager(t)
	if j := uploadAndWait(t, m, "disk.iso", "crafted.zip", crafted); j.Phase != "failed" || !strings.Contains(j.Error, "too many entries") {
		t.Fatal(j)
	}
	// Many tiny entries are refused as well.
	buf.Reset()
	archive = zip.NewWriter(&buf)
	for i := range zipMaxEntries + 1 {
		archive.Create(fmt.Sprintf("d/%d.txt", i))
	}
	w, _ = archive.Create("disk.iso")
	w.Write(testISO(40 << 10))
	archive.Close()
	if j := uploadAndWait(t, m, "disk.iso", "many.zip", buf.Bytes()); j.Phase != "failed" || !strings.Contains(j.Error, "too many entries") {
		t.Fatal(j)
	}
	// A normal archive still extracts with the directory budget in place.
	buf.Reset()
	archive = zip.NewWriter(&buf)
	w, _ = archive.Create("disk.iso")
	w.Write(testISO(3 << 20))
	archive.Close()
	old := zipDirectoryBudget
	zipDirectoryBudget = 4 << 10
	t.Cleanup(func() { zipDirectoryBudget = old })
	if j := uploadAndWait(t, m, "disk.iso", "ok.zip", buf.Bytes()); j.Phase != "done" {
		t.Fatal(j)
	}
}
