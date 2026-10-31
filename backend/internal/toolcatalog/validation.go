package toolcatalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"unicode/utf8"
)

// ValidateArguments enforces the schema subset used by the TitanOS tool
// registry before any read or plan. It never guesses missing targets or coerces
// strings into numbers/booleans. API handlers still enforce domain rules.
func ValidateArguments(schema map[string]any, raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return Errorf("invalid arguments: expected a JSON object: %v", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Errorf("invalid arguments: expected exactly one JSON object")
	}
	if _, ok := value.(map[string]any); !ok {
		return Errorf("invalid arguments: expected a JSON object")
	}
	if schema == nil {
		return nil
	}
	// Normalize Go slices and numeric schema literals to JSON types.
	data, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("invalid tool schema: %w", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return err
	}
	return validateValue(normalized, value, "arguments")
}

func validateValue(s map[string]any, value any, at string) error {
	bad := func(expect string) error {
		return Errorf("invalid arguments: %s must be %s; follow the tool schema", at, expect)
	}
	if values, ok := s["enum"].([]any); ok {
		found := false
		for _, allowed := range values {
			found = found || reflect.DeepEqual(value, allowed)
		}
		if !found {
			return bad(fmt.Sprint(values))
		}
	}
	switch s["type"] {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return bad("an object")
		}
		props, _ := s["properties"].(map[string]any)
		required, _ := s["required"].([]any)
		for _, key := range required {
			name, _ := key.(string)
			if _, ok := obj[name]; !ok {
				return Errorf("invalid arguments: %s.%s is required; follow the tool schema", at, name)
			}
		}
		for key, v := range obj {
			if property, ok := props[key].(map[string]any); ok {
				if err := validateValue(property, v, at+"."+key); err != nil {
					return err
				}
			} else if s["additionalProperties"] == false {
				return Errorf("invalid arguments: unknown field %s.%s; follow the tool schema", at, key)
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return bad("an array")
		}
		if n, ok := s["minItems"].(float64); ok && len(array) < int(n) {
			return bad(fmt.Sprintf("an array with at least %g items", n))
		}
		if n, ok := s["maxItems"].(float64); ok && len(array) > int(n) {
			return bad(fmt.Sprintf("an array with at most %g items", n))
		}
		if item, ok := s["items"].(map[string]any); ok {
			for i, v := range array {
				if err := validateValue(item, v, fmt.Sprintf("%s[%d]", at, i)); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return bad("a string")
		}
		if n, ok := s["maxLength"].(float64); ok && utf8.RuneCountInString(text) > int(n) {
			return bad(fmt.Sprintf("at most %g characters", n))
		}
	case "integer", "number":
		n, ok := value.(float64)
		if !ok {
			return bad("a number")
		}
		if s["type"] == "integer" && math.Trunc(n) != n {
			return bad("an integer")
		}
		if low, ok := s["minimum"].(float64); ok && n < low {
			return bad(fmt.Sprintf("at least %g", low))
		}
		if high, ok := s["maximum"].(float64); ok && n > high {
			return bad(fmt.Sprintf("at most %g", high))
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return bad("a boolean")
		}
	}
	return nil
}
