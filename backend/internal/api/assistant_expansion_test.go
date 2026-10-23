package api

import (
	"crypto/sha256"
	"fmt"
	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/office"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDocumentEditProtectsSourceAndOutput(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "model": "qwen3:8b"})
	original, _ := office.DOCX("# Report\n\nVersion old", "Report")
	os.WriteFile(filepath.Join(dir, "report.docx"), original, 0600)
	q := documentEdit{Root: "share/Docs", Source: "report.docx", Path: "revised.docx", SourceHash: fmt.Sprintf("%x", sha256.Sum256(original)), Edits: []office.TextEdit{{Find: "old", Replace: "new", Count: 1}}}
	res, body := ts.request("POST", "/api/v1/assistant/files/edit", q)
	if res.StatusCode != 201 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, _ = ts.request("POST", "/api/v1/assistant/files/edit", q); res.StatusCode < 400 {
		t.Fatal("overwrote output")
	}
	os.WriteFile(filepath.Join(dir, "report.docx"), []byte("changed"), 0600)
	q.Path = "another.docx"
	if res, _ = ts.request("POST", "/api/v1/assistant/files/edit", q); res.StatusCode < 400 {
		t.Fatal("edited changed source")
	}
	if _, err := os.Stat(filepath.Join(dir, "another.docx")); !os.IsNotExist(err) {
		t.Fatal("wrote despite changed source")
	}
}
func TestQueueClaimsPriorityOnce(t *testing.T) {
	now := time.Now()
	s := &Server{agentSlots: make(chan struct{}, 1), agentJobs: map[string]*agentJob{}}
	low := &agentJob{eligible: true, priority: -1, queuedAt: now}
	high := &agentJob{eligible: true, priority: 1, queuedAt: now.Add(time.Second)}
	s.agentJobs["alex/low"] = low
	s.agentJobs["alex/high"] = high
	if s.claimAgent("alex/low", low) {
		t.Fatal("low priority bypassed high")
	}
	if !s.claimAgent("alex/high", high) {
		t.Fatal("high not selected")
	}
	if s.claimAgent("alex/low", low) || s.claimAgent("alex/high", high) {
		t.Fatal("two jobs entered")
	}
	<-s.agentSlots
	delete(s.agentJobs, "alex/high")
	if !s.claimAgent("alex/low", low) {
		t.Fatal("next job blocked")
	}
}
func TestDependencyFailedAndForeignChatNeverUnlock(t *testing.T) {
	dir := t.TempDir()
	s := &Server{chats: assistant.NewChats(func() (string, bool) { return dir, true })}
	c, err := s.chats.New("alex", "dependency")
	if err != nil {
		t.Fatal(err)
	}
	c.Run = &assistant.Run{State: "done"}
	s.chats.Save("alex", c)
	job := &assistant.Chat{DependsOn: []string{c.ID}}
	if ready, _ := s.dependenciesReady("alex", job); !ready {
		t.Fatal("done dependency blocked")
	}
	if ready, _ := s.dependenciesReady("sam", job); ready {
		t.Fatal("foreign dependency unlocked")
	}
	c.Run.State = "blocked"
	s.chats.Save("alex", c)
	if ready, _ := s.dependenciesReady("alex", job); ready {
		t.Fatal("failed dependency unlocked")
	}
}
