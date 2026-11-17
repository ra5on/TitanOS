package backuptarget

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// named is the name restic gives a file: the SHA-256 of its content.
func named(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Every file but the config is named after the SHA-256 of its content, as
// rest-server checks too: an upload that arrived changed is refused and
// nothing is stored under its name.
func TestUploadMustMatchItsName(t *testing.T) {
	m, root := newTarget(t)
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	serverURL = srv.URL
	text, err := m.Pair("Backup")
	if err != nil {
		t.Fatal(err)
	}
	c := decode(t, text)
	name := named("pack")
	for _, kind := range []string{"data", "keys", "locks", "snapshots", "index"} {
		if res, _ := do(t, c, "POST", "/Backup/"+kind+"/"+name, "pack, changed on the way"); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s with a wrong hash: %d", kind, res.StatusCode)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "mnt/tank", targetDir, "Backup/data", name[:2], name)); err == nil {
		t.Error("a file that does not match its name was stored")
	}
	if res, _ := do(t, c, "POST", "/Backup/data/"+name, "pack"); res.StatusCode != http.StatusOK {
		t.Errorf("matching upload: %d", res.StatusCode)
	}
	if res, _ := do(t, c, "POST", "/Backup/config", "any content"); res.StatusCode != http.StatusOK {
		t.Errorf("config: %d", res.StatusCode)
	}
}

// A sender that stops sending in the middle of an upload does not keep the
// request (and with it the volume) open for ever.
func TestStalledUploadIsCutOff(t *testing.T) {
	m, root := newTarget(t)
	m.stall = 200 * time.Millisecond
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	text, err := m.Pair("Backup")
	if err != nil {
		t.Fatal(err)
	}
	c := decode(t, text)
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/Backup/data/"+named("pack"), nil)
	req.SetBasicAuth(c.User, c.Password)
	fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: x\r\nAuthorization: %s\r\nContent-Length: 1000\r\n\r\npa", req.URL.Path, req.Header.Get("Authorization"))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = bufio.NewReader(conn).ReadString('\n')
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("the stalled upload was still open after 5 s")
	}
	dir := filepath.Join(root, "mnt/tank", targetDir, "Backup/data")
	for range 100 {
		entries, _ := os.ReadDir(filepath.Join(dir, named("pack")[:2]))
		if len(entries) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("the part file of the stalled upload was left behind")
}
