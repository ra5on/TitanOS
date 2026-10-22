package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func validJSONResult(s string) []byte {
	if json.Valid([]byte(s)) {
		return []byte(s)
	}
	b, _ := json.Marshal(s)
	return b
}

// Verification is an observation, not a promise that an asynchronous job is
// complete. The model receives the distinction and the next status tool.
func (s *Server) verifyAgentAction(ctx context.Context, user auth.User, a *assistant.Action, r *http.Request) any {
	out := map[string]any{"observedAt": time.Now(), "state": "not_verified", "next": "Read the relevant status before claiming the requested outcome."}
	if strings.HasPrefix(a.Key, "container.") {
		status := s.opts.Docker.Status(ctx)
		for _, c := range status.Containers {
			if c.Name == a.Params["name"] {
				out["state"] = "observed"
				out["containerState"] = c.State
				out["name"] = c.Name
				out["next"] = "Compare observed state with the requested start/stop/restart. A restart's health can still be pending."
				return out
			}
		}
		out["next"] = "Container not observed; do not report success."
		return out
	}
	if strings.HasPrefix(a.Key, "vm.") {
		v, err := s.toolVMs(ctx)
		if err == nil {
			out["state"] = "observed"
			out["vms"] = v
		}
		return out
	}
	if a.Tool == "file_write" {
		var args struct{ Root, Path string }
		b, _ := json.Marshal(a.Body)
		_ = json.Unmarshal(b, &args)
		root, err := s.assistantRoot(user, args.Root)
		if err == nil {
			f, info, e := s.opts.Files.Open(root, args.Path)
			if e == nil {
				f.Close()
				out["state"] = "verified"
				out["bytes"] = info.Size()
				out["root"] = args.Root
				out["path"] = args.Path
			}
		}
		return out
	}
	if a.Tool == "file_copy" || strings.HasPrefix(a.Key, "backup.") || a.Tool == "app_action" || a.Tool == "update_action" {
		out["state"] = "accepted"
		out["next"] = "Background work may be running. Check file_jobs, backup_status, docker_list or update_status; wait for completion with agent_wait."
		return out
	}
	return out
}
