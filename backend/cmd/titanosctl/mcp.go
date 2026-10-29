package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

// bridgeMCP relays the stdio transport of MCP to the Streamable HTTP
// endpoint of the NAS (docs/MCP.md), for clients that only start local
// programs: one JSON-RPC message per line on stdin, each answer as one line
// on stdout. The token goes into the Authorization header, never into an
// argument. Messages are sent one after the other.
func bridgeMCP(client *http.Client, endpoint, token string, in io.Reader, out, logs io.Writer) error {
	if t, ok := client.Transport.(*http.Transport); ok {
		// A tool call may run up to two minutes on the NAS.
		t = t.Clone()
		t.ResponseHeaderTimeout = 3 * time.Minute
		copy := *client
		copy.Transport = t
		client = &copy
	}
	reader := bufio.NewReaderSize(in, 64<<10)
	version := ""
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 8<<20 {
			return fmt.Errorf("message exceeds 8 MiB")
		}
		if msg := bytes.TrimSpace(line); len(msg) > 0 {
			answers, negotiated, failure := relayMCP(client, endpoint, token, version, msg)
			if negotiated != "" {
				version = negotiated
			}
			if failure != nil {
				fmt.Fprintln(logs, "titanosctl mcp:", failure)
				answers = failedAnswers(msg, failure)
			}
			for _, answer := range answers {
				var compact bytes.Buffer
				if json.Compact(&compact, answer) != nil {
					continue
				}
				compact.WriteByte('\n')
				if _, err := out.Write(compact.Bytes()); err != nil {
					return err
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// relayMCP posts one message and returns the JSON-RPC answers and, for an
// initialize, the negotiated protocol version.
func relayMCP(client *http.Client, endpoint, token, version string, msg []byte) ([]json.RawMessage, string, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(msg))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if version != "" {
		req.Header.Set("MCP-Protocol-Version", version)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > 32<<20 {
		return nil, "", fmt.Errorf("answer exceeds 32 MiB")
	}
	if resp.StatusCode == http.StatusAccepted {
		return nil, "", nil
	}
	if resp.StatusCode >= 300 {
		// Refusals (switched off, token, rate limit) carry no usable id.
		return nil, "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	var answers []json.RawMessage
	switch {
	case contentType == "text/event-stream":
		answers = sseData(data)
	case json.Valid(data) && bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")):
		json.Unmarshal(data, &answers)
	case json.Valid(data) && strings.Contains(string(data), `"jsonrpc"`):
		answers = []json.RawMessage{data}
	}
	if len(answers) == 0 {
		return nil, "", fmt.Errorf("the NAS sent no JSON-RPC answer")
	}
	negotiated := ""
	if isInitialize(msg) {
		for _, answer := range answers {
			var v struct {
				Result struct {
					ProtocolVersion string `json:"protocolVersion"`
				} `json:"result"`
			}
			if json.Unmarshal(answer, &v) == nil && v.Result.ProtocolVersion != "" {
				negotiated = v.Result.ProtocolVersion
			}
		}
	}
	return answers, negotiated, nil
}

func isInitialize(msg []byte) bool {
	var v struct {
		Method string `json:"method"`
	}
	return json.Unmarshal(msg, &v) == nil && v.Method == "initialize"
}

// sseData returns the data of the events of a stream.
func sseData(data []byte) []json.RawMessage {
	var answers []json.RawMessage
	for _, event := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n\n") {
		var lines []string
		for _, line := range strings.Split(event, "\n") {
			if v, ok := strings.CutPrefix(line, "data:"); ok {
				lines = append(lines, strings.TrimPrefix(v, " "))
			}
		}
		if text := strings.Join(lines, "\n"); text != "" && json.Valid([]byte(text)) {
			answers = append(answers, json.RawMessage(text))
		}
	}
	return answers
}

// failedAnswers turns a failed relay into JSON-RPC errors for the requests
// of the message, so that the client does not wait forever.
func failedAnswers(msg []byte, failure error) []json.RawMessage {
	var requests []struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if bytes.HasPrefix(msg, []byte("[")) {
		json.Unmarshal(msg, &requests)
	} else {
		requests = make([]struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}, 1)
		json.Unmarshal(msg, &requests[0])
	}
	var answers []json.RawMessage
	for _, r := range requests {
		if len(r.ID) == 0 || string(r.ID) == "null" || r.Method == "" {
			continue
		}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": r.ID, "error": map[string]any{"code": -32000, "message": failure.Error()}})
		answers = append(answers, data)
	}
	return answers
}
