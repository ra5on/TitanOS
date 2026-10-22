package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	for _, raw := range []string{`null`, `[]`, `"{broken"`, `{"`} {
		if ValidateArguments(nil, argumentsOf(json.RawMessage(raw))) == nil {
			t.Errorf("malformed arguments became executable: %s", raw)
		}
	}
}

func TestModelOutputRecovery(t *testing.T) {
	for _, scenario := range []string{"http-recovery", "stream-recovery", "exhausted", "truncated", "unrelated-500"} {
		t.Run(scenario, func(t *testing.T) {
			requests, executed := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					w.Write([]byte(`{"capabilities":["tools"]}`))
					return
				}
				requests++
				if scenario == "exhausted" || requests == 1 {
					switch scenario {
					case "truncated":
						w.Write([]byte(`{"message":{"tool_calls":[{"function":{"name":"test","arguments":{}}}]}}`))
						return
					case "unrelated-500":
						w.WriteHeader(500)
						w.Write([]byte(`{"error":"out of memory"}`))
						return
					case "stream-recovery":
						w.Write([]byte("{\"message\":{\"thinking\":\"partial thought\"}}\n"))
					default:
						w.WriteHeader(500)
					}
					w.Write([]byte(`{"error":"error parsing tool call: unexpected end of JSON input"}`))
					return
				}
				if requests == 2 {
					w.Write([]byte(`{"message":{"tool_calls":[{"function":{"name":"test","arguments":{}}}]},"done":true}`))
					return
				}
				w.Write([]byte(`{"message":{"content":"done"},"done":true}`))
			}))
			defer srv.Close()
			m := manager(t, srv.URL)
			chat := newChat(t, "test")
			err := m.Answer(context.Background(), chat, Turn{Model: "qwen3:8b", Emit: func(Event) {}, Tools: []Tool{{Name: "test", Read: func(context.Context, json.RawMessage) (Result, error) { executed++; return Result{Text: "ok"}, nil }}}})
			switch scenario {
			case "http-recovery", "stream-recovery":
				if err != nil || executed != 1 || requests != 3 {
					t.Fatalf("err=%v executed=%d requests=%d", err, executed, requests)
				}
				if chat.Messages[1].ErrorCode != "model_tool_parse" {
					t.Fatalf("missing recovery evidence: %+v", chat.Messages)
				}
			case "exhausted":
				if !errors.Is(err, ErrModelOutput) || requests != 3 || executed != 0 {
					t.Fatalf("err=%v requests=%d executed=%d", err, requests, executed)
				}
			default:
				if err == nil || requests != 1 || executed != 0 {
					t.Fatalf("err=%v requests=%d executed=%d", err, requests, executed)
				}
			}
		})
	}
}

func TestInvalidArgumentsNeverDispatch(t *testing.T) {
	fake := &fakeOllama{replies: [][]string{{`{"tool_calls":[{"function":{"name":"erase","arguments":null}}]}`}, {`{"content":"cannot execute"}`}}}
	m := manager(t, fake.serve(t))
	chat := newChat(t, "test")
	err := m.Answer(context.Background(), chat, Turn{Model: "qwen3:8b", Emit: func(Event) {}, Tools: []Tool{{Name: "erase", Plan: func(context.Context, json.RawMessage) (*Action, error) {
		t.Fatal("invalid call dispatched")
		return nil, nil
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(chat)
	if !strings.Contains(string(b), "invalid_arguments") {
		t.Fatal(string(b))
	}
}

func TestRepeatedInvalidCallsCannotLoopForever(t *testing.T) {
	for _, name := range []string{"known", "unknown"} {
		t.Run(name, func(t *testing.T) {
			call := `{"tool_calls":[{"function":{"name":"` + name + `","arguments":null}}]}`
			fake := &fakeOllama{replies: [][]string{{call}, {call}, {call}, {call}, {`{"content":"Unable to perform the request."}`}}}
			m := manager(t, fake.serve(t))
			chat := newChat(t, "test")
			err := m.Answer(t.Context(), chat, Turn{Model: "qwen3:8b", Emit: func(Event) {}, Tools: []Tool{{Name: "known", Read: func(context.Context, json.RawMessage) (Result, error) {
				t.Fatal("invalid call dispatched")
				return Result{}, nil
			}}}})
			if err != nil || len(fake.got) != 5 {
				t.Fatalf("err=%v requests=%d", err, len(fake.got))
			}
		})
	}
}

func TestParserRecoveryNeverRepeatsCompletedMutation(t *testing.T) {
	requests, executed := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			w.Write([]byte(`{"capabilities":["tools"]}`))
			return
		}
		requests++
		switch requests {
		case 1:
			w.Write([]byte(`{"message":{"tool_calls":[{"function":{"name":"change","arguments":{}}}]},"done":true}`))
		case 2:
			w.WriteHeader(500)
			w.Write([]byte(`{"error":"error parsing tool call: unexpected end of JSON input"}`))
		default:
			w.Write([]byte(`{"message":{"content":"finished"},"done":true}`))
		}
	}))
	defer srv.Close()
	m := manager(t, srv.URL)
	settings := m.Settings()
	settings.Execution = "direct"
	if err := m.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	chat := newChat(t, "change once")
	err := m.Answer(t.Context(), chat, Turn{Model: "qwen3:8b", Emit: func(Event) {},
		Tools: []Tool{{Name: "change", Plan: func(context.Context, json.RawMessage) (*Action, error) {
			return &Action{Key: "change", Method: "POST", Path: "/synthetic", Level: LevelNormal}, nil
		}}},
		Execute: func(_ context.Context, _ *Chat, a *Action) error {
			executed++
			a.State = ActionDone
			a.Result = "completed"
			return nil
		},
	})
	if err != nil || requests != 3 || executed != 1 {
		t.Fatalf("err=%v requests=%d executed=%d", err, requests, executed)
	}
}
