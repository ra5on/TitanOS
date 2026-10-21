// Package mcp answers the Model Context Protocol over HTTP, in the
// stateless form of the "Streamable HTTP" transport: every POST carries
// one JSON-RPC message, every answer is plain JSON. There are no event
// streams, no sessions and no messages from the server on its own. Only
// tools are offered; see docs/AGENT-API.md.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
)

// ProtocolVersion is the version of the specification this follows.
const ProtocolVersion = "2025-06-18"

// supported are the versions a client may ask for; for others the server
// offers its own, as the specification says.
var supported = []string{"2025-03-26", ProtocolVersion}

// Tool is one thing an agent may call.
type Tool struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	// InputSchema is a JSON schema of type object.
	InputSchema map[string]any `json:"inputSchema"`
	Annotations *Annotations   `json:"annotations,omitempty"`
	// Call does the work. Args are the arguments as sent, never nil. An
	// error is told to the agent as the result of the tool, so that it
	// can try again; it is not a protocol error.
	Call func(ctx context.Context, args json.RawMessage) (any, error) `json:"-"`
}

// Annotations are hints about a tool for the client.
type Annotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

// ReadOnly are the annotations of a tool that only reads.
var ReadOnly = &Annotations{ReadOnlyHint: true, IdempotentHint: true}

// ToolError is a mistake of the agent, such as a wrong argument. Its
// message is shown to the agent as is.
type ToolError struct{ Message string }

func (e *ToolError) Error() string { return e.Message }

// Errorf returns a ToolError.
func Errorf(format string, args ...any) error {
	return &ToolError{Message: fmt.Sprintf(format, args...)}
}

// Server answers MCP requests.
type Server struct {
	Name    string
	Version string
	// Instructions tell the agent what the server is for.
	Instructions string
	// OnCall is told about every call of a tool, for the log.
	OnCall func(ctx context.Context, tool string, err error)
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeNoMethod       = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRPCError(w http.ResponseWriter, status int, id json.RawMessage, code int, message string) {
	if id == nil {
		id = json.RawMessage("null")
	}
	writeJSON(w, status, response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
}

// Serve answers one HTTP request with the given tools. The caller has
// checked who is asking and limited the size of the body.
func (s *Server) Serve(w http.ResponseWriter, r *http.Request, tools []Tool) {
	if r.Method != http.MethodPost {
		// No stream from the server: there is nothing it would send on its own.
		w.Header().Set("Allow", http.MethodPost)
		writeRPCError(w, http.StatusMethodNotAllowed, nil, codeInvalidRequest, "only POST is supported")
		return
	}
	if version := r.Header.Get("MCP-Protocol-Version"); version != "" && !slices.Contains(supported, version) {
		writeRPCError(w, http.StatusBadRequest, nil, codeInvalidRequest, "unsupported MCP-Protocol-Version "+version)
		return
	}
	var body bytes.Buffer
	if _, err := body.ReadFrom(r.Body); err != nil {
		writeRPCError(w, http.StatusRequestEntityTooLarge, nil, codeInvalidRequest, "request too large")
		return
	}
	raw := bytes.TrimSpace(body.Bytes())
	if len(raw) > 0 && raw[0] == '[' {
		writeRPCError(w, http.StatusBadRequest, nil, codeInvalidRequest, "batches are not supported")
		return
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, codeParse, "not valid JSON")
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		// A response from the client: this server never asks anything.
		if req.JSONRPC == "2.0" && req.Method == "" && req.ID != nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeRPCError(w, http.StatusBadRequest, req.ID, codeInvalidRequest, "not a JSON-RPC 2.0 request")
		return
	}
	if req.ID == nil || string(req.ID) == "null" {
		// A notification, such as notifications/initialized: nothing to answer.
		w.WriteHeader(http.StatusAccepted)
		return
	}

	result, rpcErr := s.handle(r.Context(), req, tools)
	if rpcErr != nil {
		writeJSON(w, http.StatusOK, response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr})
		return
	}
	writeJSON(w, http.StatusOK, response{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func (s *Server) handle(ctx context.Context, req request, tools []Tool) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		version := ProtocolVersion
		if slices.Contains(supported, params.ProtocolVersion) {
			version = params.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "title": "TitanOS", "version": s.Version},
			"instructions":    s.Instructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		// All tools fit on one page.
		list := make([]Tool, len(tools))
		copy(list, tools)
		return map[string]any{"tools": list}, nil
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "params must name a tool"}
		}
		i := slices.IndexFunc(tools, func(tool Tool) bool { return tool.Name == params.Name })
		if i < 0 {
			return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + params.Name}
		}
		args := params.Arguments
		if len(args) == 0 || string(args) == "null" {
			args = json.RawMessage("{}")
		}
		data, err := tools[i].Call(ctx, args)
		if s.OnCall != nil {
			s.OnCall(ctx, params.Name, err)
		}
		return toolResult(data, err)
	default:
		return nil, &rpcError{Code: codeNoMethod, Message: "method not found: " + req.Method}
	}
}

// toolResult puts the data of a tool both as structured content and, for
// clients that only read text, as the same JSON in a text block.
func toolResult(data any, err error) (any, *rpcError) {
	if err != nil {
		message := "the tool failed"
		var toolErr *ToolError
		if errors.As(err, &toolErr) {
			message = toolErr.Message
		}
		return map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": message}}}, nil
	}
	text, err := json.Marshal(data)
	if err != nil {
		return nil, &rpcError{Code: codeInternal, Message: "the result could not be encoded"}
	}
	return map[string]any{
		"content":           []map[string]string{{"type": "text", "text": string(text)}},
		"structuredContent": data,
	}, nil
}

// DecodeArgs reads the arguments of a tool strictly, so that a misspelt
// argument is told to the agent instead of being ignored.
func DecodeArgs(args json.RawMessage, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return Errorf("invalid arguments: %v", err)
	}
	return nil
}
