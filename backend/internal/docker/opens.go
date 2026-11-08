package docker

// „Öffnen mit“ für Docker-Apps (PLAN 72): An app's template says in
// x-titanos.opens which file types it opens and which program in its main
// container does so, for example
//
//	x-titanos:
//	  opens:
//	    types: [3mf, stl, step]
//	    command: [bambu-studio, "{file}"]
//	    user: abc
//	    env: [DISPLAY=:1]
//
// titand starts that program with the file's path inside the container.
// The file must lie in a folder mounted into the container; MountShare
// mounts a share for it (owner and administrators only, the app then sees
// the whole share). The command is an argument list, never a shell line;
// "{file}" is replaced by exactly one argument.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// FilePlaceholder is replaced by the path of the file in the container.
const FilePlaceholder = "{file}"

// SharesTarget is where MountShare mounts shares in a container.
const SharesTarget = "/shares"

const (
	maxOpenTypes  = 32
	maxOpenArgs   = 16
	maxOpenEnv    = 16
	openStartWait = 30 * time.Second
)

// openStartAfter is the moment a just started app gets to bring up its
// desktop before the program is run (shorter in tests).
var openStartAfter = 3 * time.Second

var (
	// ErrNotMounted: the file lies in no folder the container sees.
	ErrNotMounted = errors.New("docker: the file is not in a folder of the app")
	// ErrNoOpener: the app opens no files (or not this type).
	ErrNoOpener = errors.New("docker: the app opens no such files")
	// ErrMounted: the share is mounted in the app already.
	ErrMounted = errors.New("docker: the share is mounted in the app already")

	openTypeRE  = regexp.MustCompile(`^[a-z0-9]{1,10}$`)
	openUserRE  = regexp.MustCompile(`^[a-z0-9_][a-z0-9_.-]{0,31}(:[a-z0-9_][a-z0-9_.-]{0,31})?$`)
	openEnvRE   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}=[^\x00\n]{0,256}$`)
	shareNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`)
)

// Opener is the x-titanos.opens of an app.
type Opener struct {
	Types   []string `json:"types"`
	Command []string `json:"command"`
	User    string   `json:"user,omitempty"`
	Env     []string `json:"env,omitempty"`
	// Service is the container the program runs in: opens.service, else
	// x-titanos.main, else the only service.
	Service string `json:"service"`
	// Hidden keeps the app out of the „Öffnen mit“ menu (Docker › Bearbeiten).
	Hidden bool `json:"hidden,omitempty"`
}

// Validate checks an opener as the template or the editor gives it.
func (o *Opener) Validate() error {
	if len(o.Types) == 0 || len(o.Types) > maxOpenTypes {
		return fmt.Errorf("%w: opens.types needs 1 to %d file types", ErrInvalid, maxOpenTypes)
	}
	for _, t := range o.Types {
		if !openTypeRE.MatchString(t) {
			return fmt.Errorf("%w: opens.types: %q is no file type (lower case, without dot)", ErrInvalid, t)
		}
	}
	if len(o.Command) < 2 || len(o.Command) > maxOpenArgs {
		return fmt.Errorf("%w: opens.command needs a program and %q", ErrInvalid, FilePlaceholder)
	}
	placeholders := 0
	for i, arg := range o.Command {
		if arg == "" || len(arg) > 512 || strings.ContainsAny(arg, "\x00\n") {
			return fmt.Errorf("%w: opens.command has an empty or odd argument", ErrInvalid)
		}
		if strings.Contains(arg, FilePlaceholder) {
			if arg != FilePlaceholder || i == 0 {
				return fmt.Errorf("%w: %q must be an argument of its own", ErrInvalid, FilePlaceholder)
			}
			placeholders++
		}
	}
	if placeholders != 1 {
		return fmt.Errorf("%w: opens.command needs %q exactly once", ErrInvalid, FilePlaceholder)
	}
	if o.User != "" && !openUserRE.MatchString(o.User) {
		return fmt.Errorf("%w: opens.user is no user name or UID:GID", ErrInvalid)
	}
	if len(o.Env) > maxOpenEnv {
		return fmt.Errorf("%w: opens.env has more than %d entries", ErrInvalid, maxOpenEnv)
	}
	for _, e := range o.Env {
		if !openEnvRE.MatchString(e) {
			return fmt.Errorf("%w: opens.env entries are NAME=value", ErrInvalid)
		}
	}
	return nil
}

// Opens reports whether the opener takes files of this type.
func (o *Opener) Opens(ext string) bool { return slices.Contains(o.Types, strings.ToLower(ext)) }

// argv is the command for a file inside the container.
func (o *Opener) argv(file string) []string {
	out := slices.Clone(o.Command)
	for i, arg := range out {
		if arg == FilePlaceholder {
			out[i] = file
		}
	}
	return out
}

// parseOpener reads x-titanos.opens of a compose file; nil without one.
// A broken opens block is an error, so that the editor can say so.
func parseOpener(compose string) (*Opener, string, error) {
	var doc struct {
		Meta struct {
			Name  string `yaml:"name"`
			Main  string `yaml:"main"`
			Opens *struct {
				Types   []string `yaml:"types"`
				Command []string `yaml:"command"`
				User    string   `yaml:"user"`
				Env     []string `yaml:"env"`
				Service string   `yaml:"service"`
				Hidden  bool     `yaml:"hidden"`
			} `yaml:"opens"`
		} `yaml:"x-titanos"`
		Services map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(compose), &doc); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	name := doc.Meta.Name
	o := doc.Meta.Opens
	if o == nil {
		return nil, name, nil
	}
	op := &Opener{Command: o.Command, User: o.User, Env: o.Env, Service: o.Service, Hidden: o.Hidden}
	for _, t := range o.Types {
		op.Types = append(op.Types, strings.TrimPrefix(strings.ToLower(strings.TrimSpace(t)), "."))
	}
	if op.Service == "" {
		op.Service = doc.Meta.Main
	}
	if op.Service == "" && len(doc.Services) == 1 {
		for s := range doc.Services {
			op.Service = s
		}
	}
	if _, ok := doc.Services[op.Service]; !ok {
		return nil, name, fmt.Errorf("%w: opens.service names no service of the app", ErrInvalid)
	}
	if err := op.Validate(); err != nil {
		return nil, name, err
	}
	return op, name, nil
}

type openEntry struct {
	op   *Opener
	name string
	err  error
}

// opener returns the cached opener of an app; the compose file on the
// stick is read once per app until the app changes.
func (m *Manager) opener(app string) openEntry {
	m.mu.Lock()
	entry, ok := m.opens[app]
	m.mu.Unlock()
	if ok {
		return entry
	}
	compose, err := m.Compose(app)
	if err != nil {
		return openEntry{err: err}
	}
	op, name, err := parseOpener(compose)
	entry = openEntry{op: op, name: name, err: err}
	m.mu.Lock()
	if m.opens == nil {
		m.opens = map[string]openEntry{}
	}
	m.opens[app] = entry
	m.mu.Unlock()
	return entry
}

// forgetApps drops what is known of the apps in /config. Call with m.mu held.
func (m *Manager) forgetApps() {
	m.appList = nil
	m.opens = nil
}

// Mount is a folder of the NAS inside a container.
type Mount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly"`
}

// AppOpener is an app that opens files, as the file manager lists it.
type AppOpener struct {
	App  string `json:"app"`
	Name string `json:"name"`
	// The opens block; empty when it is broken (Error).
	Opener
	Running bool    `json:"running"`
	Mounts  []Mount `json:"mounts"`
	// Error says why the opens block of the app is not used.
	Error string `json:"error,omitempty"`
}

// Openers lists the managed apps with an opens block.
func (m *Manager) Openers(ctx context.Context) ([]AppOpener, error) {
	out := []AppOpener{}
	if m.Disabled {
		return out, nil
	}
	m.mu.Lock()
	apps := m.knownApps()
	m.mu.Unlock()
	var items []apiContainer
	for _, app := range apps {
		entry := m.opener(app)
		if entry.op == nil && entry.err == nil {
			continue
		}
		if errors.Is(entry.err, ErrNotFound) {
			continue
		}
		item := AppOpener{App: app, Name: entry.name, Mounts: []Mount{}}
		if item.Name == "" {
			item.Name = app
		}
		if entry.err != nil {
			item.Error = entry.err.Error()
			out = append(out, item)
			continue
		}
		item.Opener = *entry.op
		if items == nil {
			var err error
			if items, err = m.api.containers(ctx); err != nil {
				return nil, err
			}
		}
		if c, ok := serviceContainer(items, app, entry.op.Service); ok {
			item.Running = c.State == "running"
			mounts, _, err := m.containerMounts(ctx, c.ID)
			if err != nil {
				return nil, err
			}
			item.Mounts = mounts
		}
		out = append(out, item)
	}
	return out, nil
}

// serviceContainer finds the container of a service of a compose app.
func serviceContainer(items []apiContainer, app, service string) (apiContainer, bool) {
	for _, c := range items {
		if c.Labels["com.docker.compose.project"] == app && c.Labels["com.docker.compose.service"] == service {
			return c, true
		}
	}
	return apiContainer{}, false
}

// containerMounts are the bind mounts of a container and whether it runs.
func (m *Manager) containerMounts(ctx context.Context, id string) ([]Mount, bool, error) {
	var inspect struct {
		Mounts []struct {
			Type        string `json:"Type"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
			RW          bool   `json:"RW"`
		} `json:"Mounts"`
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err := m.api.getJSON(ctx, "/containers/"+url.PathEscape(id)+"/json", nil, &inspect); err != nil {
		return nil, false, err
	}
	out := []Mount{}
	for _, mt := range inspect.Mounts {
		if mt.Type == "bind" && mt.Source != "" && mt.Destination != "" {
			out = append(out, Mount{Source: mt.Source, Target: mt.Destination, ReadOnly: !mt.RW})
		}
	}
	return out, inspect.State.Running, nil
}

// ContainerPath maps a file of the NAS to its path in the container: the
// mount with the longest source that holds it. False if none does.
func ContainerPath(mounts []Mount, host string) (string, Mount, bool) {
	host = filepath.Clean(host)
	if !filepath.IsAbs(host) {
		return "", Mount{}, false
	}
	var best Mount
	var rest string
	found := false
	for _, mt := range mounts {
		src := filepath.Clean(mt.Source)
		rel, err := filepath.Rel(src, host)
		if err != nil || rel == "." || !filepath.IsLocal(rel) {
			continue
		}
		if !found || len(src) > len(filepath.Clean(best.Source)) {
			best, rest, found = mt, rel, true
		}
	}
	if !found {
		return "", Mount{}, false
	}
	return path.Join(best.Target, filepath.ToSlash(rest)), best, true
}

// OpenFile starts the app's program for a file of the NAS (an absolute,
// already checked path). A stopped app is started first.
func (m *Manager) OpenFile(ctx context.Context, app, host string) error {
	if m.Disabled {
		return ErrDisabled
	}
	if !appNameRE.MatchString(app) {
		return ErrNotFound
	}
	entry := m.opener(app)
	if entry.err != nil {
		return entry.err
	}
	if entry.op == nil || !entry.op.Opens(strings.TrimPrefix(filepath.Ext(host), ".")) {
		return ErrNoOpener
	}
	items, err := m.api.containers(ctx)
	if err != nil {
		return err
	}
	c, ok := serviceContainer(items, app, entry.op.Service)
	if !ok {
		return ErrNotFound
	}
	mounts, running, err := m.containerMounts(ctx, c.ID)
	if err != nil {
		return err
	}
	file, _, ok := ContainerPath(mounts, host)
	if !ok {
		return ErrNotMounted
	}
	if !running {
		if err := m.Action(ctx, c.ID, "start"); err != nil {
			return err
		}
		if err := m.waitRunning(ctx, c.ID); err != nil {
			return err
		}
	}
	return m.api.execDetached(ctx, c.ID, entry.op.argv(file), entry.op.User, entry.op.Env)
}

// waitRunning waits until a started container runs and has had a moment
// to bring up its desktop.
func (m *Manager) waitRunning(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, openStartWait)
	defer cancel()
	for {
		_, running, err := m.containerMounts(ctx, id)
		if err != nil {
			return err
		}
		if running {
			select {
			case <-ctx.Done():
			case <-time.After(openStartAfter):
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("docker: the app did not start: %w", ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// execDetached runs a command in a container without waiting for it and
// without a terminal (the program of „Öffnen mit“).
func (c *client) execDetached(ctx context.Context, id string, cmd []string, user string, env []string) error {
	request := map[string]any{"AttachStdin": false, "AttachStdout": false, "AttachStderr": false, "Tty": false, "Cmd": cmd}
	if user != "" {
		request["User"] = user
	}
	if len(env) > 0 {
		request["Env"] = env
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := c.postJSON(ctx, "/containers/"+url.PathEscape(id)+"/exec", request, &created); err != nil {
		return err
	}
	return c.postJSON(ctx, "/exec/"+url.PathEscape(created.ID)+"/start", map[string]any{"Detach": true, "Tty": false}, nil)
}

// postJSON posts a JSON body and decodes a JSON answer into out (if not nil).
func (c *client) postJSON(ctx context.Context, p string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker/v1.43"+p, bytes.NewReader(data))
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
		var failure struct{ Message string }
		_ = json.NewDecoder(io.LimitReader(res.Body, 64*1024)).Decode(&failure) // a value that cannot be read leaves the zero value
		return &apiError{Status: res.StatusCode, Message: failure.Message}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64*1024)) // drained for the connection's reuse; nothing in it matters
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}

// ShareTarget is where a share is mounted in a container.
func ShareTarget(share string) string { return SharesTarget + "/" + share }

// MountShare adds a share (its folder on the NAS) to the opens service of
// an app and recreates it. readOnly mounts it read-only.
func (m *Manager) MountShare(ctx context.Context, app, share, source string, readOnly bool) error {
	if !shareNameRE.MatchString(share) || !filepath.IsAbs(source) || filepath.Clean(source) != source || strings.ContainsAny(source, ":\n\x00") {
		return ErrInvalid
	}
	entry := m.opener(app)
	if entry.err != nil {
		return entry.err
	}
	if entry.op == nil {
		return ErrNoOpener
	}
	compose, err := m.Compose(app)
	if err != nil {
		return err
	}
	next, err := editVolumes(compose, entry.op.Service, func(list *yaml.Node) error {
		target := ShareTarget(share)
		for _, item := range list.Content {
			if volumeTarget(item) == target {
				return ErrMounted
			}
		}
		value := source + ":" + target
		if readOnly {
			value += ":ro"
		}
		list.Content = append(list.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: value})
		return nil
	})
	if err != nil {
		return err
	}
	return m.SetCompose(ctx, app, next)
}

// UnmountShare removes a share that MountShare added.
func (m *Manager) UnmountShare(ctx context.Context, app, share string) error {
	if !shareNameRE.MatchString(share) {
		return ErrInvalid
	}
	entry := m.opener(app)
	if entry.err != nil {
		return entry.err
	}
	if entry.op == nil {
		return ErrNoOpener
	}
	compose, err := m.Compose(app)
	if err != nil {
		return err
	}
	next, err := editVolumes(compose, entry.op.Service, func(list *yaml.Node) error {
		target := ShareTarget(share)
		kept := list.Content[:0]
		removed := false
		for _, item := range list.Content {
			if volumeTarget(item) == target {
				removed = true
				continue
			}
			kept = append(kept, item)
		}
		if !removed {
			return ErrNotFound
		}
		list.Content = kept
		return nil
	})
	if err != nil {
		return err
	}
	return m.SetCompose(ctx, app, next)
}

// volumeTarget is the path in the container of a volumes entry, short
// ("src:dst[:ro]") or long form ({target: …}).
func volumeTarget(item *yaml.Node) string {
	switch item.Kind {
	case yaml.ScalarNode:
		parts := strings.Split(item.Value, ":")
		if len(parts) >= 2 {
			return path.Clean(parts[1])
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(item.Content); i += 2 {
			if item.Content[i].Value == "target" {
				return path.Clean(item.Content[i+1].Value)
			}
		}
	}
	return ""
}

// editVolumes changes the volumes list of a service and returns the compose
// file; comments elsewhere stay.
func editVolumes(compose, service string, edit func(list *yaml.Node) error) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(compose), &doc); err != nil || len(doc.Content) == 0 {
		return "", ErrInvalid
	}
	services := mapValue(doc.Content[0], "services")
	if services == nil {
		return "", ErrInvalid
	}
	svc := mapValue(services, service)
	if svc == nil || svc.Kind != yaml.MappingNode {
		return "", ErrInvalid
	}
	list := mapValue(svc, "volumes")
	if list == nil {
		list = &yaml.Node{Kind: yaml.SequenceNode}
		svc.Content = append(svc.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "volumes"}, list)
	}
	if list.Kind != yaml.SequenceNode {
		return "", ErrInvalid
	}
	if err := edit(list); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// SetOpener replaces x-titanos.opens of an app (Docker › Bearbeiten).
// Only the template block changes; the containers stay as they are.
func (m *Manager) SetOpener(ctx context.Context, app string, op Opener) error {
	entry := m.opener(app)
	if entry.err != nil && !errors.Is(entry.err, ErrInvalid) {
		return entry.err
	}
	compose, err := m.Compose(app)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(compose), &doc); err != nil || len(doc.Content) == 0 {
		return ErrInvalid
	}
	root := doc.Content[0]
	services := mapValue(root, "services")
	if services == nil || mapValue(services, op.Service) == nil {
		return fmt.Errorf("%w: opens.service names no service of the app", ErrInvalid)
	}
	for i, t := range op.Types {
		op.Types[i] = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(t)), ".")
	}
	if err := op.Validate(); err != nil {
		return err
	}
	var block yaml.Node
	if err := block.Encode(map[string]any{"types": op.Types, "command": op.Command}); err != nil {
		return err
	}
	add := func(key string, value any) {
		var v yaml.Node
		_ = v.Encode(value) // plain strings, bools and lists always encode
		block.Content = append(block.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &v)
	}
	if op.User != "" {
		add("user", op.User)
	}
	if len(op.Env) > 0 {
		add("env", op.Env)
	}
	add("service", op.Service)
	if op.Hidden {
		add("hidden", true)
	}
	meta := mapValue(root, "x-titanos")
	if meta == nil {
		meta = &yaml.Node{Kind: yaml.MappingNode}
		root.Content = append([]*yaml.Node{{Kind: yaml.ScalarNode, Value: "x-titanos"}, meta}, root.Content...)
	}
	if old := mapValue(meta, "opens"); old != nil {
		*old = block
	} else {
		meta.Content = append(meta.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "opens"}, &block)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	return m.SetCompose(ctx, app, buf.String())
}

// mapValue is the value of a key in a YAML mapping, or nil.
func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// ValidAppName reports whether a name can be that of a managed app.
func ValidAppName(name string) bool { return appNameRE.MatchString(name) }
