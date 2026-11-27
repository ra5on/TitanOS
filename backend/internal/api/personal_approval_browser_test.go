package api

import (
	"bytes"
	"context"
	"encoding/json"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This optional part of the delivery integration test uses the same browser
// entry points and private worker protocol as an installation. All mail stays
// inside the test's IMAP and SMTP servers.
func testDeliveryApprovalUI(t *testing.T, ts *testServer, j pa.Job, mailbox string) {
	t.Helper()
	if resp, _ := ts.request("POST", "/api/v1/system/disclaimer", map[string]int{"version": 1}); resp.StatusCode >= 300 {
		t.Fatal(resp.StatusCode)
	}
	if resp, _ := ts.request("PUT", "/api/v1/preferences/hints", map[string]bool{"webapps-0.63": true}); resp.StatusCode != 204 {
		t.Fatal(resp.StatusCode)
	}
	u, err := url.Parse(ts.url)
	if err != nil {
		t.Fatal(err)
	}
	cookies := map[string]string{}
	for _, c := range ts.client.Jar.Cookies(u) {
		cookies[c.Name] = c.Value
	}
	input, err := json.Marshal(map[string]any{"origin": ts.url, "job": j.ID, "mailbox": mailbox, "cookies": cookies})
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(t.Context(), 3*time.Minute)
	socket := filepath.Join(t.TempDir(), "agent.sock")
	brokerEnd, workerEnd := make(chan error, 1), make(chan error, 1)
	go func() { brokerEnd <- ts.server.ServePersonalBroker(ctx, socket, os.Geteuid()) }()
	go func() { workerEnd <- pa.RunWorker(ctx, socket) }()
	defer func() {
		stop()
		if err := <-workerEnd; err != nil {
			t.Error(err)
		}
		if err := <-brokerEnd; err != nil {
			t.Error(err)
		}
	}()
	cmd := exec.CommandContext(ctx, "node", "../../../scripts/test-ui-personal-approval.mjs")
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}
