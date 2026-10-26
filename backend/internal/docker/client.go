package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// client talks to the Docker Engine API over its unix socket. Only the
// standard library is used; the API is plain HTTP with JSON.
type client struct {
	http   *http.Client
	socket string
}

func newClient(socket string) *client {
	return &client{socket: socket, http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}}
}

// apiError is an error answer of the engine.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return fmt.Sprintf("docker: %s (HTTP %d)", e.Message, e.Status) }

func (c *client) do(ctx context.Context, method, path string, query url.Values) (*http.Response, error) {
	target := "http://docker/v1.43" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		defer res.Body.Close()
		var body struct{ Message string }
		json.NewDecoder(io.LimitReader(res.Body, 64*1024)).Decode(&body)
		return nil, &apiError{Status: res.StatusCode, Message: body.Message}
	}
	return res, nil
}

func (c *client) getJSON(ctx context.Context, path string, query url.Values, v any) error {
	res, err := c.do(ctx, http.MethodGet, path, query)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return json.NewDecoder(res.Body).Decode(v)
}

func (c *client) sendJSON(ctx context.Context, method, path string, body, v any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker/v1.43"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var problem struct{ Message string }
		json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&problem)
		return &apiError{Status: res.StatusCode, Message: problem.Message}
	}
	if v != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(v)
	}
	return nil
}

func (c *client) post(ctx context.Context, path string, query url.Values) error {
	res, err := c.do(ctx, http.MethodPost, path, query)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

// apiContainer is an entry of GET /containers/json.
type apiContainer struct {
	ID    string   `json:"Id"`
	Names []string `json:"Names"`
	Image string   `json:"Image"`
	State string   `json:"State"`
	// ImageID is the image the container was created from.
	ImageID string            `json:"ImageID"`
	Labels  map[string]string `json:"Labels"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

// apiInspect is the part of GET /containers/{id}/json that is used.
type apiInspect struct {
	Mounts []struct{ Source string }
	Config struct {
		Env    []string
		Labels map[string]string
	}
	HostConfig struct {
		Privileged     bool
		Runtime        string
		Devices        []struct{ PathOnHost string }
		DeviceRequests []struct {
			Driver       string
			Count        int
			DeviceIDs    []string
			Capabilities [][]string
		}
	}

	State struct {
		Running    bool      `json:"Running"`
		StartedAt  time.Time `json:"StartedAt"`
		FinishedAt time.Time `json:"FinishedAt"`
	} `json:"State"`
}

// apiStats is the part of GET /containers/{id}/stats that is used.
type apiStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
}

func (c *client) version(ctx context.Context) (string, error) {
	var v struct{ Version string }
	err := c.getJSON(ctx, "/version", nil, &v)
	return v.Version, err
}

func (c *client) containers(ctx context.Context) ([]apiContainer, error) {
	var list []apiContainer
	err := c.getJSON(ctx, "/containers/json", url.Values{"all": {"true"}}, &list)
	return list, err
}

// stats returns one sample without waiting for a second one; the caller
// compares it with the previous sample.
func (c *client) stats(ctx context.Context, id string) (apiStats, error) {
	var s apiStats
	err := c.getJSON(ctx, "/containers/"+id+"/stats", url.Values{"stream": {"false"}, "one-shot": {"true"}}, &s)
	return s, err
}

// logs returns the last lines a container wrote to stdout and stderr.
func (c *client) logs(ctx context.Context, id string, tail int) (string, error) {
	res, err := c.do(ctx, http.MethodGet, "/containers/"+id+"/logs", url.Values{
		"stdout": {"true"}, "stderr": {"true"}, "tail": {fmt.Sprint(tail)}, "timestamps": {"true"},
	})
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	return demux(bufio.NewReader(io.LimitReader(res.Body, 4<<20)))
}

// demux decodes the stream format of the logs endpoint: frames with an
// 8-byte header (stream, 3 zero bytes, length) followed by the text. A
// container with a terminal sends plain text instead.
func demux(r *bufio.Reader) (string, error) {
	var out strings.Builder
	for {
		header, err := r.Peek(8)
		if errors.Is(err, io.EOF) && len(header) == 0 {
			return out.String(), nil
		}
		if err != nil || header[0] > 2 || header[1] != 0 || header[2] != 0 || header[3] != 0 {
			// Not framed: plain text of a container with a terminal.
			rest, _ := io.ReadAll(r)
			out.Write(rest)
			return out.String(), nil
		}
		r.Discard(8)
		size := binary.BigEndian.Uint32(header[4:8])
		if _, err := io.CopyN(&out, r, int64(size)); err != nil {
			return out.String(), nil
		}
	}
}

// imageDigests returns the registry digests an image is known under.
func (c *client) imageDigests(ctx context.Context, id string) ([]string, error) {
	var image struct {
		RepoDigests []string `json:"RepoDigests"`
	}
	if err := c.getJSON(ctx, "/images/"+url.PathEscape(id)+"/json", nil, &image); err != nil {
		return nil, err
	}
	var digests []string
	for _, entry := range image.RepoDigests {
		if _, digest, ok := strings.Cut(entry, "@"); ok {
			digests = append(digests, digest)
		}
	}
	return digests, nil
}

// remoteDigest asks the registry what an image reference currently points to.
func (c *client) remoteDigest(ctx context.Context, ref string) (string, error) {
	var info struct {
		Descriptor struct {
			Digest string `json:"digest"`
		} `json:"Descriptor"`
	}
	// The reference may contain slashes and must stay part of the path.
	err := c.getJSON(ctx, "/distribution/"+ref+"/json", nil, &info)
	return info.Descriptor.Digest, err
}

// execCreate prepares a command to run inside a container, with a terminal.
func (c *client) execCreate(ctx context.Context, id string, cmd []string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"AttachStdin": true, "AttachStdout": true, "AttachStderr": true, "Tty": true, "Cmd": cmd,
		"Env": []string{"TERM=xterm-256color"},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker/v1.43/containers/"+id+"/exec", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var failure struct{ Message string }
		json.NewDecoder(io.LimitReader(res.Body, 64*1024)).Decode(&failure)
		return "", &apiError{Status: res.StatusCode, Message: failure.Message}
	}
	var created struct {
		ID string `json:"Id"`
	}
	err = json.NewDecoder(res.Body).Decode(&created)
	return created.ID, err
}

// execStart runs a prepared command and returns the connection to its
// terminal: what is written goes to the command, what it prints comes back.
// Docker hands over the raw connection for this, which net/http's client
// cannot do, so the request is written by hand.
func (c *client) execStart(ctx context.Context, execID string) (io.ReadWriteCloser, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", c.socket)
	if err != nil {
		return nil, err
	}
	const body = `{"Detach":false,"Tty":true}`
	_, err = fmt.Fprintf(conn, "POST /v1.43/exec/%s/start HTTP/1.1\r\nHost: docker\r\nContent-Type: application/json\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: %d\r\n\r\n%s", execID, len(body), body)
	if err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if res.StatusCode != http.StatusSwitchingProtocols && res.StatusCode != http.StatusOK {
		conn.Close()
		return nil, &apiError{Status: res.StatusCode, Message: "exec could not be started"}
	}
	// The reader may already hold the first bytes of output.
	return &hijacked{Conn: conn, reader: reader}, nil
}

type hijacked struct {
	net.Conn
	reader *bufio.Reader
}

func (h *hijacked) Read(p []byte) (int, error) { return h.reader.Read(p) }

func (c *client) execResize(ctx context.Context, execID string, cols, rows int) error {
	return c.post(ctx, "/exec/"+execID+"/resize", url.Values{"w": {fmt.Sprint(cols)}, "h": {fmt.Sprint(rows)}})
}
