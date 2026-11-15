package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/shares"
)

// code_run (PLAN 55 point 8, decision of the owner of 5.11.2026: the
// middle way): the agent runs a short Python or sh program in a sandbox
// container without network that sees only the account's own folder of
// the share KI-Agent as /work (docker/sandbox.go). Off by default, only
// with Docker switched on, and it asks before every run unless the tool or
// the area "code" is set to "Ohne Nachfrage" by name. Not a console: no
// terminal, nothing of the NAS outside the folder, nothing stays running.

const codeMax = 32 << 10

// codeTool is code_run, or nil when it is not offered.
func (b *agentToolbox) codeTool() *agentTool {
	env := b.s.agentEnv()
	if !b.settings.Code.Enabled || !env.servicesOn()["docker"] || (env.docker == nil && b.s.agentSandbox == nil) {
		return nil
	}
	return &agentTool{name: "code_run", area: agent.AreaCode, kind: agent.KindChange, untrusted: true,
		description: "Run a short program in a sandbox without network: Python 3 or sh (busybox). Besides the standard library Python has " + docker.SandboxLibsList + " to read and change Word, Excel and PDF files. Your folder of the share KI-Agent is the working folder /work (read and write); nothing else of the NAS is visible: copy a file of another share there first (file_copy), change it, and copy the result back. Use it to calculate, convert, sort or check data and for changes to documents that document_edit cannot do (insert rows, formulas, merge PDFs); print the result. Limits: " + fmt.Sprint(b.settings.Code.Timeout) + " s, " + fmt.Sprint(b.settings.Code.Memory) + " MiB.",
		params: schema(map[string]any{
			"language": enum("python (default) or sh.", "python", "sh"),
			"code":     str("The program. Print what you need to see."),
		}, "code"),
		own: b.codeRun}
}

// codeMode: code_run asks unless it is allowed by name or by its area.
func (b *agentToolbox) codeMode(mode string) string {
	p := b.settings.Policy
	if p.Tools["code_run"] == agent.ModeAllow || (p.Tools["code_run"] == "" && p.Areas[agent.AreaCode] == agent.ModeAllow) {
		return mode
	}
	return agent.Stricter(mode, agent.ModeAsk)
}

type codeArgs struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

func parseCode(args json.RawMessage) (codeArgs, error) {
	var req codeArgs
	if err := json.Unmarshal(args, &req); err != nil {
		return req, errors.New("arguments: language and code")
	}
	if req.Language == "" {
		req.Language = "python"
	}
	if req.Language != "python" && req.Language != "sh" {
		return req, errors.New("language: python or sh")
	}
	if strings.TrimSpace(req.Code) == "" || len(req.Code) > codeMax {
		return req, errors.New("code: 1 to 32768 characters")
	}
	return req, nil
}

func (b *agentToolbox) codeRun(ctx context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	env := b.s.agentEnv()
	req, err := parseCode(args)
	if err != nil {
		return agent.Outcome{}, err
	}
	sh, ok := b.s.agentShare()
	if !ok || env.shares == nil {
		return agent.Outcome{}, errors.New("the share KI-Agent is not there (no data volume?)")
	}
	root, err := b.s.agentUserRoot(sh, b.user)
	if err != nil {
		return agent.Outcome{}, fmt.Errorf("folder of the account: %v", err)
	}
	root.Close()
	run := b.s.agentSandbox
	if run == nil {
		run = env.docker.Sandbox
	}
	// The libraries are loaded by the first run that finds none; a run
	// goes on without them when that fails (no internet) and says so.
	libs, libsErr := b.s.sandboxLibs(ctx, sh)
	res, err := run(ctx, docker.SandboxRequest{Language: req.Language, Code: req.Code, Work: env.shares.UserFolder(sh, b.user.Name),
		UID: b.user.UID, GID: b.s.usersGID(), Timeout: time.Duration(b.settings.Code.Timeout) * time.Second, MemoryMB: b.settings.Code.Memory, User: b.user.Name, Libs: libs})
	if err != nil {
		return agent.Outcome{}, err
	}
	var out strings.Builder
	if libsErr != nil {
		fmt.Fprintf(&out, "Note: the libraries for Word, Excel and PDF are not there (%v); only the standard library works.\n", libsErr)
	}
	switch {
	case res.TimedOut:
		fmt.Fprintf(&out, "Stopped after %d s (time limit).\n", b.settings.Code.Timeout)
	default:
		fmt.Fprintf(&out, "Exit code %d after %.1f s.\n", res.ExitCode, res.Took.Seconds())
	}
	if strings.TrimSpace(res.Output) == "" {
		out.WriteString("(no output)")
	} else {
		out.WriteString(res.Output)
	}
	return agent.Outcome{Text: out.String()}, nil
}

// sandboxLibs returns the folder with the libraries of the sandbox on the
// volume of the share KI-Agent (.titanos/agent/sandbox-libs-<n>), loading
// them on the first call.
func (s *Server) sandboxLibs(ctx context.Context, sh shares.Share) (string, error) {
	env := s.agentEnv()
	if env.shares == nil {
		return "", nil
	}
	dir := filepath.Join(filepath.Dir(env.shares.Path(sh)), ".titanos", "agent", "sandbox-libs-"+docker.SandboxLibsVersion)
	if s.agentSandbox != nil || env.docker == nil {
		// Tests replace the sandbox; they see the libraries only when a
		// test laid them there.
		return docker.SandboxLibsReady(dir), nil
	}
	return env.docker.SandboxLibs(ctx, dir)
}

// codeSummary is the short form for notifications and the note in the
// chat: the start of the program. The approval card shows the whole
// program from the arguments (ChatView.svelte, preview), not this.
func codeSummary(args json.RawMessage) string {
	req, err := parseCode(args)
	if err != nil {
		return "code_run"
	}
	return "code_run (" + req.Language + "):\n" + clipRunes(req.Code, 1500)
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
