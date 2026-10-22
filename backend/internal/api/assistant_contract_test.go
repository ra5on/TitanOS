package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// Exercise every advertised contract, including nested document/move schemas,
// without dispatching destructive operations against a real NAS.
func TestAssistantEntireToolCatalog(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	settings := ts.server.opts.Assistant.Settings()
	settings.Web = true
	settings.WebUsers = true
	settings.Areas = map[string]string{}
	for _, area := range assistant.Areas {
		settings.Areas[area] = assistant.AccessWrite
	}
	if err := ts.server.opts.Assistant.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, role := range []auth.Role{auth.RoleOwner, auth.RoleAdmin, auth.RoleUser} {
		user := auth.User{Name: "alex", Role: role}
		tools := ts.server.agentTools(user, httptest.NewRequest("GET", "/", nil), &assistant.Chat{Run: &assistant.Run{}}, nil)
		seen := map[string]bool{}
		for _, tool := range tools {
			t.Run(string(role)+"/"+tool.Name, func(t *testing.T) {
				if seen[tool.Name] || tool.Description == "" || (tool.Read == nil) == (tool.Plan == nil) {
					t.Fatalf("invalid registration: %+v", tool)
				}
				seen[tool.Name] = true
				b, err := json.Marshal(tool.Parameters)
				if err != nil {
					t.Fatal(err)
				}
				var schema map[string]any
				json.Unmarshal(b, &schema)
				sample := schemaSample(schema).(map[string]any)
				raw, _ := json.Marshal(sample)
				if err := assistant.ValidateArguments(tool.Parameters, raw); err != nil {
					t.Fatalf("schema contradicts sample %s: %v", raw, err)
				}
				for _, bad := range []string{`null`, `[]`, `"{}"`, `{} {}`} {
					if assistant.ValidateArguments(tool.Parameters, json.RawMessage(bad)) == nil {
						t.Errorf("accepted %s", bad)
					}
				}
				props, _ := schema["properties"].(map[string]any)
				for key := range props {
					original := sample[key]
					sample[key] = nil
					raw, _ := json.Marshal(sample)
					if assistant.ValidateArguments(tool.Parameters, raw) == nil {
						t.Errorf("accepted null for %s", key)
					}
					sample[key] = original
				}
				required, _ := schema["required"].([]any)
				for _, key := range required {
					name := key.(string)
					original := sample[name]
					delete(sample, name)
					raw, _ := json.Marshal(sample)
					if assistant.ValidateArguments(tool.Parameters, raw) == nil {
						t.Errorf("accepted missing %s", name)
					}
					sample[name] = original
				}
			})
		}
		t.Logf("%s: %d tools checked", role, len(tools))
	}
}

func schemaSample(s map[string]any) any {
	if choices, ok := s["enum"].([]any); ok && len(choices) > 0 {
		return choices[0]
	}
	switch s["type"] {
	case "object":
		value := map[string]any{}
		props, _ := s["properties"].(map[string]any)
		for key, p := range props {
			value[key] = schemaSample(p.(map[string]any))
		}
		return value
	case "array":
		count := 1
		if n, ok := s["minItems"].(float64); ok {
			count = int(n)
		}
		result := []any{}
		item, _ := s["items"].(map[string]any)
		for i := 0; i < count; i++ {
			result = append(result, schemaSample(item))
		}
		return result
	case "integer", "number":
		if n, ok := s["minimum"].(float64); ok {
			return n
		}
		return 1
	case "boolean":
		return false
	default:
		return "sample"
	}
}

func TestAssistantAreaOffCoversStatusTools(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	settings := ts.server.opts.Assistant.Settings()
	settings.Areas = map[string]string{}
	for _, area := range assistant.Areas {
		settings.Areas[area] = assistant.AccessOff
	}
	if err := ts.server.opts.Assistant.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, role := range []auth.Role{auth.RoleOwner, auth.RoleUser} {
		tools := ts.server.assistantTools(auth.User{Name: "alex", Role: role}, httptest.NewRequest("GET", "/", nil))
		for _, tool := range tools {
			switch tool.Name {
			case "docs_search", "docs_read", "web_search", "web_read", "api_routes", "api_read", "api_change":
			default:
				t.Errorf("disabled NAS tool remains available: %s", tool.Name)
			}
		}
	}
}

func TestAssistantSnapshotConfirmationContract(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	user, _ := ts.users.Get("alex")
	tools := ts.server.apiTools(user, httptest.NewRequest("GET", "/", nil))
	for _, tool := range tools {
		if tool.Name != "api_change" {
			continue
		}
		for _, test := range []struct{ method, path, confirm string }{
			{"POST", "/vms/testvm/snapshots/save/restore", "testvm"},
			{"DELETE", "/vms/testvm/snapshots/save", "Before update"},
		} {
			raw, _ := json.Marshal(map[string]any{"method": test.method, "path": test.path, "summary": "requested restore/delete", "reason": "requested", "body": map[string]any{"confirm": test.confirm}})
			action, err := tool.Plan(t.Context(), raw)
			if err != nil {
				t.Fatal(err)
			}
			if action.Confirm != test.confirm || action.ConfirmField != "body:confirm" {
				t.Fatalf("wrong confirmation: %+v", action)
			}
		}
	}
}

func TestAssistantMutationPathsAreNeverRetargeted(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	user, _ := ts.users.Get("alex")
	for _, bad := range []string{"../photo.jpg", "a/../../photo.jpg", "/photo.jpg", "a\\photo.jpg"} {
		if _, err := ts.server.fileTarget(user, "share/Docs", bad, true); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got, err := ts.server.fileTarget(user, "share/Docs", " photo.jpg ", true); err != nil || got != " photo.jpg " {
		t.Fatalf("changed filename: %q %v", got, err)
	}
}

func TestAssistantDiagnosticsDistinguishMessagesAndToolErrors(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	settings := ts.server.opts.Assistant.Settings()
	settings.Enabled = true
	if err := ts.server.opts.Assistant.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	id := ts.newChat(ts.client)
	c, err := ts.server.chats.Get("alex", id)
	if err != nil {
		t.Fatal(err)
	}
	c.Messages = []assistant.Entry{
		{Role: "user", Content: "private question"},
		{Role: "assistant", Content: "private thought"},
		{Role: "tool", Tool: "files_search", ErrorCode: "root_not_found", Content: "private filename", Args: json.RawMessage(`{"root":"private"}`)},
	}
	if err := ts.server.chats.Save("alex", c); err != nil {
		t.Fatal(err)
	}
	_, data := ts.request("GET", "/api/v1/assistant/chats/"+id+"/diagnostics", nil)
	var report struct{ Steps []map[string]any }
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	for _, step := range report.Steps[:2] {
		if _, ok := step["ok"]; ok {
			t.Fatalf("chat message mislabeled as failure: %s", data)
		}
	}
	step := report.Steps[2]
	if step["errorCode"] != "root_not_found" || step["ok"] != false {
		t.Fatalf("missing error category: %s", data)
	}
	if strings.Contains(string(data), "private") {
		t.Fatalf("content leaked: %s", data)
	}
}
