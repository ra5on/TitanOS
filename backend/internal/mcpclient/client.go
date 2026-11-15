// Package mcpclient is a small client for servers of the Model Context
// Protocol over "Streamable HTTP": the KI-Agent connects to servers the
// owner entered (n8n, Home Assistant, a tool of one's own …) and offers
// their tools beside its own (owner 15.11.2026). Only what the agent
// needs: initialize, list the tools, call one. A server answers with JSON
// or with a stream of events; both are read. Nothing is kept open.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Version is the protocol version the client asks for.
const Version = "2025-06-18"

// maxBody bounds an answer of a server.
const maxBody = 4 << 20

// Tool is a tool of a server.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// Client talks to one server.
type Client struct {
	URL   string
	Token string
	// HTTP is the client to use; nil takes one with a time limit.
	HTTP *http.Client

	mu      sync.Mutex
	session string
	version string
	ready   bool
	next    int
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcAnswer struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// send posts one message; with wait it reads the answer to id.
func (c *Client) send(ctx context.Context, method string, params any, notify bool, out any) error {
	c.mu.Lock()
	c.next++
	id := c.next
	session, version := c.session, c.version
	c.mu.Unlock()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	if !notify {
		msg["id"] = id
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	if version != "" {
		req.Header.Set("MCP-Protocol-Version", version)
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if s := res.Header.Get("Mcp-Session-Id"); s != "" {
		c.mu.Lock()
		c.session = s
		c.mu.Unlock()
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return fmt.Errorf("the server refused the sign-in (%d): check the token", res.StatusCode)
	case res.StatusCode == http.StatusNotFound && session != "":
		// The session ended on the server; the next call starts a new one.
		c.mu.Lock()
		c.session, c.ready = "", false
		c.mu.Unlock()
		return errors.New("the session on the server ended; try again")
	case res.StatusCode >= 300:
		text, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("the server answered %d %s", res.StatusCode, strings.TrimSpace(string(text)))
	}
	if notify {
		return nil
	}
	answer, err := readAnswer(io.LimitReader(res.Body, maxBody), res.Header.Get("Content-Type"), id)
	if err != nil {
		return err
	}
	if answer.Error != nil {
		return fmt.Errorf("%s (%d)", answer.Error.Message, answer.Error.Code)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(answer.Result, out)
}

// readAnswer finds the answer to id in a JSON body or a stream of events.
func readAnswer(body io.Reader, contentType string, id int) (rpcAnswer, error) {
	want := fmt.Sprint(id)
	match := func(raw []byte) (rpcAnswer, bool) {
		var a rpcAnswer
		if json.Unmarshal(raw, &a) != nil || strings.Trim(string(a.ID), `"`) != want || (a.Result == nil && a.Error == nil) {
			return a, false
		}
		return a, true
	}
	if !strings.HasPrefix(contentType, "text/event-stream") {
		raw, err := io.ReadAll(body)
		if err != nil {
			return rpcAnswer{}, err
		}
		if a, ok := match(raw); ok {
			return a, nil
		}
		return rpcAnswer{}, errors.New("the server did not answer with a result")
	}
	scan := bufio.NewScanner(body)
	scan.Buffer(make([]byte, 64<<10), maxBody)
	var data []byte
	for scan.Scan() {
		line := scan.Text()
		switch {
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")...)
		case line == "":
			if a, ok := match(data); ok {
				return a, nil
			}
			data = data[:0]
		}
	}
	if a, ok := match(data); ok {
		return a, nil
	}
	return rpcAnswer{}, errors.New("the stream ended without a result")
}

// start initializes the connection once.
func (c *Client) start(ctx context.Context) error {
	c.mu.Lock()
	ready := c.ready
	c.mu.Unlock()
	if ready {
		return nil
	}
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	err := c.send(ctx, "initialize", map[string]any{
		"protocolVersion": Version,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "TitanOS KI-Agent", "version": "1"},
	}, false, &init)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.version, c.ready = init.ProtocolVersion, true
	c.mu.Unlock()
	return c.send(ctx, "notifications/initialized", nil, true, nil)
}

// Tools lists the tools of the server (at most 200).
func (c *Client) Tools(ctx context.Context) ([]Tool, error) {
	if err := c.start(ctx); err != nil {
		return nil, err
	}
	var all []Tool
	cursor := ""
	for page := 0; page < 5; page++ {
		var out struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		if err := c.send(ctx, "tools/list", params, false, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Tools...)
		if cursor = out.NextCursor; cursor == "" || len(all) >= 200 {
			break
		}
	}
	if len(all) > 200 {
		all = all[:200]
	}
	return all, nil
}

// Call runs a tool and returns the text of its answer; failed reports an
// error the tool itself returned (isError).
func (c *Client) Call(ctx context.Context, name string, args json.RawMessage) (text string, failed bool, err error) {
	if err := c.start(ctx); err != nil {
		return "", false, err
	}
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}
	if err := c.send(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, false, &out); err != nil {
		return "", false, err
	}
	var b strings.Builder
	for _, part := range out.Content {
		switch part.Type {
		case "text":
			b.WriteString(part.Text)
			b.WriteString("\n")
		default:
			fmt.Fprintf(&b, "[%s content left out]\n", part.Type)
		}
	}
	if b.Len() == 0 && len(out.Structured) > 0 {
		b.Write(out.Structured)
	}
	return strings.TrimSpace(b.String()), out.IsError, nil
}
