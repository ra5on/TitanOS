package api

import (
	"encoding/json"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Exercise every advertised contract, including nested document/move schemas,
// without dispatching destructive operations against a real NAS.
func TestCatalogEntireToolCatalog(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	for _, role := range []auth.Role{auth.RoleOwner, auth.RoleAdmin, auth.RoleUser} {
		user := auth.User{Name: "alex", Role: role}
		tools := ts.server.catalogTools(user)
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
				if err := toolcatalog.ValidateArguments(tool.Parameters, raw); err != nil {
					t.Fatalf("schema contradicts sample %s: %v", raw, err)
				}
				for _, bad := range []string{`null`, `[]`, `"{}"`, `{} {}`} {
					if toolcatalog.ValidateArguments(tool.Parameters, json.RawMessage(bad)) == nil {
						t.Errorf("accepted %s", bad)
					}
				}
				props, _ := schema["properties"].(map[string]any)
				for key := range props {
					original := sample[key]
					sample[key] = nil
					raw, _ := json.Marshal(sample)
					if toolcatalog.ValidateArguments(tool.Parameters, raw) == nil {
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
					if toolcatalog.ValidateArguments(tool.Parameters, raw) == nil {
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

func TestCatalogMutationPathsAreNeverRetargeted(t *testing.T) {
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
