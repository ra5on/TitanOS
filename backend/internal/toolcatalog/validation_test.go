package toolcatalog

import (
	"encoding/json"
	"testing"
)

func TestArgumentValidation(t *testing.T) {
	schema := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"root"}, "properties": map[string]any{
		"root": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer", "minimum": 0},
		"mode":  map[string]any{"type": "string", "enum": []string{"images", "text"}},
		"paths": map[string]any{"type": "array", "minItems": 1, "maxItems": 2, "items": map[string]any{"type": "string"}},
	}}
	for _, raw := range []string{`null`, `[]`, `{} {}`, `{"root":null}`, `{"root":"x","offset":-1}`, `{"root":"x","offset":0.5}`, `{"root":"x","offset":"1"}`, `{"root":"x","mode":"bogus"}`, `{"root":"x","paths":[]}`, `{"root":"x","paths":[1]}`, `{"root":"x","extra":true}`, `{}`} {
		if err := ValidateArguments(schema, json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if err := ValidateArguments(schema, json.RawMessage(`{"root":"share/Media","offset":0,"paths":["a"]}`)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`null`, `{} {}`, `[]`} {
		if DecodeArgs(json.RawMessage(raw), &struct{}{}) == nil {
			t.Errorf("DecodeArgs accepted %s", raw)
		}
	}
}
