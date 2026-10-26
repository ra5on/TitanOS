// Package homeassistant connects TitanOS to a Home Assistant server: the
// assistant reads and switches entities that were released to it, messages
// go to the Home Assistant app, and state changes start automations.
//
// The connection is stored in /config/homeassistant/settings.json. The
// long-lived token in it never leaves the NAS through the API. Only the
// configured server is ever contacted, also when it is in the home network,
// and redirects are not followed.
package homeassistant

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const settingsFile = "homeassistant/settings.json"

var (
	// ErrInvalid means a setting is not acceptable; the message says which.
	ErrInvalid = errors.New("homeassistant: invalid setting")
	// ErrOff means no server is set up or the connection is switched off.
	ErrOff = errors.New("homeassistant: not set up")
	// ErrNotAllowed means an entity or service was not released to TitanOS.
	ErrNotAllowed = errors.New("homeassistant: not released")
	// ErrUnreachable wraps a failed request to the server.
	ErrUnreachable = errors.New("homeassistant: server unreachable")
	// ErrNotLoaded means /config could not be read; nothing is written then.
	ErrNotLoaded = errors.New("homeassistant: settings not loaded from /config")
)

// Settings is /config/homeassistant/settings.json.
type Settings struct {
	Enabled bool `json:"enabled"`
	// URL is the address of Home Assistant, e.g. http://homeassistant.local:8123.
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
	// Fingerprint is the SHA-256 of the server's certificate in hex. With
	// it, a self-signed certificate is accepted, but only that one.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Domains and Entities are what the assistant may see and switch.
	Domains  []string                `json:"domains"`
	Entities []string                `json:"entities"`
	Policies map[string]EntityPolicy `json:"policies,omitempty"`
	// Account is the TitanOS account that buttons in Home Assistant and
	// questions from Home Assistant Assist act for. Its rights apply.
	Account string `json:"account,omitempty"`
	// Assist answers questions from Home Assistant Assist (event titanos_ask).
	Assist bool `json:"assist,omitempty"`
	// MQTT publishes sensors of the NAS to Home Assistant.
	MQTT MQTT `json:"mqtt"`
}

// MQTT is the broker Home Assistant's MQTT integration uses.
type MQTT struct {
	// ExtraGroups is a storage compatibility field. The live/API Groups contains
	// every selected group; older apps only see their known Groups on disk.
	ExtraGroups []string            `json:"extraGroups,omitempty"`
	Devices     map[string][]string `json:"devices,omitempty"`
	Enabled     bool                `json:"enabled"`
	Host        string              `json:"host"`
	Port        int                 `json:"port"`
	Username    string              `json:"username"`
	Password    string              `json:"password,omitempty"`
	TLS         bool                `json:"tls"`
	// Prefix is Home Assistant's discovery prefix, homeassistant by default.
	Prefix string `json:"prefix"`
	// Groups of sensors: system, volumes, disks, containers, vms, ups.
	Groups []string `json:"groups"`
	// Commands are groups of buttons and switches in Home Assistant:
	// containers, vms, updates, power. None by default.
	Commands []string `json:"commands"`
	// ID names this NAS in topics and unique IDs. It is made once and kept,
	// because the machine ID of a system in RAM changes with every start.
	ID string `json:"id,omitempty"`
}

// SensorGroups and CommandGroups are the choices in MQTT.
var (
	legacySensorGroups = []string{"system", "volumes", "disks", "containers", "vms", "ups", "backups", "work"}
	SensorGroups       = append(slices.Clone(legacySensorGroups), "network", "maintenance", "stacks", "gpu", "igpu", "npu", "capacity")
	CommandGroups      = []string{"containers", "vms", "backups", "updates", "power"}
)

// Levels of a service call, as the assistant's actions know them.
const (
	LevelNormal = "normal"
	LevelDanger = "danger"
)

// domainLevels are the domains the assistant can be given, and how careful
// a call there has to be. Anything else is never offered.
var domainLevels = map[string]string{
	"light": LevelNormal, "switch": LevelNormal, "scene": LevelNormal, "script": LevelNormal,
	"media_player": LevelNormal, "climate": LevelNormal, "fan": LevelNormal, "humidifier": LevelNormal,
	"input_boolean": LevelNormal, "input_number": LevelNormal, "input_select": LevelNormal, "input_button": LevelNormal,
	"cover": LevelDanger, "lock": LevelDanger, "alarm_control_panel": LevelDanger, "vacuum": LevelDanger,
	"button": LevelDanger, "valve": LevelDanger, "water_heater": LevelDanger,
	// Reading only: sensors have no services worth calling.
	"sensor": "", "binary_sensor": "", "weather": "", "person": "", "device_tracker": "", "sun": "", "zone": "",
}

// Domains lists the domains that can be released, sorted.
func Domains() []string {
	list := make([]string, 0, len(domainLevels))
	for d := range domainLevels {
		list = append(list, d)
	}
	sort.Strings(list)
	return list
}

// Level says how a call in a domain is confirmed; ok is false where calls
// are not possible at all.
func Level(domain string) (level string, ok bool) {
	level, known := domainLevels[domain]
	return level, known && level != ""
}

var (
	entityID    = regexp.MustCompile(`^[a-z_]+\.[a-z0-9_]+$`)
	serviceName = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	fingerprint = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ValidEntity says whether s looks like an entity ID.
func ValidEntity(s string) bool { return len(s) <= 255 && entityID.MatchString(s) }

// ValidService says whether s is the name of a service, without its domain.
func ValidService(s string) bool { return serviceName.MatchString(s) }

// Manager holds the settings and talks to the server.
type Manager struct {
	store *config.Store

	mu       sync.Mutex
	loaded   bool
	settings Settings
	client   *http.Client
	// changed is closed and replaced when the settings change, for watchers.
	changed chan struct{}

	watch watchState
}

// NewManager returns a manager without a server.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, changed: make(chan struct{})}
}

// Load reads the settings from /config.
func (m *Manager) Load() error {
	var settings Settings
	if err := m.store.ReadJSON(settingsFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	mqttDefaults(&settings.MQTT)
	for _, group := range settings.MQTT.ExtraGroups {
		if slices.Contains(SensorGroups, group) {
			settings.MQTT.Groups = append(settings.MQTT.Groups, group)
		}
	}
	settings.MQTT.Groups = slices.Compact(slices.Sorted(slices.Values(settings.MQTT.Groups)))
	settings.MQTT.ExtraGroups = nil
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings, m.loaded = settings, true
	m.client = newClient(settings)
	m.notifyChange()
	return nil
}

func (m *Manager) notifyChange() {
	close(m.changed)
	m.changed = make(chan struct{})
}

// Changed returns a channel that is closed at the next change of the settings.
func (m *Manager) Changed() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.changed
}

// Settings returns the settings without the token and the MQTT password;
// the map says which of them are set.
func (m *Manager) Settings() (Settings, map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings
	s.Policies = clonePolicies(s.Policies)
	set := map[string]bool{"token": s.Token != "", "mqtt": s.MQTT.Password != ""}
	s.Token, s.MQTT.Password = "", ""
	if s.MQTT.Devices != nil {
		copied := make(map[string][]string, len(s.MQTT.Devices))
		for group, ids := range s.MQTT.Devices {
			copied[group] = append([]string{}, ids...)
		}
		s.MQTT.Devices = copied
	}
	s.MQTT.Groups, s.MQTT.Commands = append([]string{}, s.MQTT.Groups...), append([]string{}, s.MQTT.Commands...)
	s.Domains, s.Entities = append([]string{}, s.Domains...), append([]string{}, s.Entities...)
	return s, set
}

// Active says whether a server is set up and switched on.
func (m *Manager) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings.Enabled && m.settings.URL != "" && m.settings.Token != ""
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// SetSettings stores new settings. An empty token or MQTT password keeps
// the stored one; clear names those to drop: token, mqtt.
func (m *Manager) SetSettings(s Settings, clear ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if s.Token == "" && !slices.Contains(clear, "token") {
		s.Token = m.settings.Token
	}
	if s.MQTT.Password == "" && !slices.Contains(clear, "mqtt") {
		s.MQTT.Password = m.settings.MQTT.Password
	}
	s.MQTT.ExtraGroups = nil // only Load interprets the on-disk compatibility field
	if err := checkMQTT(&s.MQTT, m.settings.MQTT.ID); err != nil {
		return err
	}
	if s.Account != "" && !accountName.MatchString(s.Account) {
		return invalid("account")
	}
	s.Token = strings.TrimSpace(s.Token)
	s.URL = strings.TrimRight(strings.TrimSpace(s.URL), "/")
	if s.URL != "" {
		u, err := url.Parse(s.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(s.URL) > 300 {
			return invalid("address: use http://host:8123 or https://host")
		}
	}
	if s.Enabled && (s.URL == "" || s.Token == "") {
		return invalid("address and token are needed")
	}
	if len(s.Token) > 1024 || strings.ContainsAny(s.Token, " \r\n") {
		return invalid("token")
	}
	s.Fingerprint = strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(s.Fingerprint))
	if s.Fingerprint != "" && !fingerprint.MatchString(s.Fingerprint) {
		return invalid("certificate fingerprint: 64 hex digits (SHA-256)")
	}
	s.Domains = slices.Compact(slices.Sorted(slices.Values(s.Domains)))
	for _, d := range s.Domains {
		if _, known := domainLevels[d]; !known {
			return invalid("domain %q", d)
		}
	}
	s.Entities = slices.Compact(slices.Sorted(slices.Values(s.Entities)))
	if s.Domains == nil {
		s.Domains = []string{}
	}
	if s.Entities == nil {
		s.Entities = []string{}
	}
	if len(s.Entities) > 500 {
		return invalid("at most 500 entities")
	}
	for _, e := range s.Entities {
		if !ValidEntity(e) {
			return invalid("entity %q", e)
		}
		if _, known := domainLevels[domainOf(e)]; !known {
			return invalid("entity %q: its domain cannot be released", e)
		}
	}
	if err := validatePolicies(s.Policies); err != nil {
		return err
	}
	s.Policies = clonePolicies(s.Policies)
	persisted := s
	persisted.MQTT.Groups = []string{}
	for _, group := range s.MQTT.Groups {
		if slices.Contains(legacySensorGroups, group) {
			persisted.MQTT.Groups = append(persisted.MQTT.Groups, group)
		} else {
			persisted.MQTT.ExtraGroups = append(persisted.MQTT.ExtraGroups, group)
		}
	}
	if err := m.store.WriteJSON(settingsFile, persisted); err != nil {
		return err
	}
	m.settings = s
	m.client = newClient(s)
	m.notifyChange()
	return nil
}

var (
	mqttHost    = regexp.MustCompile(`^[A-Za-z0-9.\-:\[\]]{1,253}$`)
	mqttPrefix  = regexp.MustCompile(`^[A-Za-z0-9_\-]{1,64}(/[A-Za-z0-9_\-]{1,64})*$`)
	accountName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	mqttID      = regexp.MustCompile(`^[a-z0-9]{4,32}$`)
)

// mqttDefaults fills what files of older versions leave out.
func mqttDefaults(q *MQTT) {
	if q.Prefix == "" {
		q.Prefix = "homeassistant"
	}
	if q.Port == 0 {
		q.Port = map[bool]int{false: 1883, true: 8883}[q.TLS]
	}
	if q.Groups == nil {
		q.Groups = []string{}
	}
	if q.Commands == nil {
		q.Commands = []string{}
	}
}

func checkMQTT(q *MQTT, old string) error {
	mqttDefaults(q)
	if q.ID == "" {
		q.ID = old
	}
	if q.ID == "" {
		b := make([]byte, 6)
		rand.Read(b)
		q.ID = hex.EncodeToString(b)
	}
	if !mqttID.MatchString(q.ID) {
		return invalid("MQTT ID")
	}
	devices := make(map[string][]string, len(q.Devices))
	for group, ids := range q.Devices {
		if !slices.Contains(SensorGroups, group) || len(ids) > 256 {
			return invalid("MQTT device selection")
		}
		for _, id := range ids {
			if id == "" || len(id) > 256 || strings.ContainsAny(id, "\r\n\x00") {
				return invalid("MQTT device ID")
			}
		}
		devices[group] = slices.Compact(slices.Sorted(slices.Values(ids)))
	}
	q.Devices = devices
	q.Groups = slices.Compact(slices.Sorted(slices.Values(q.Groups)))
	q.Commands = slices.Compact(slices.Sorted(slices.Values(q.Commands)))
	for _, g := range q.Groups {
		if !slices.Contains(SensorGroups, g) {
			return invalid("sensor group %q", g)
		}
	}
	for _, g := range q.Commands {
		if !slices.Contains(CommandGroups, g) {
			return invalid("command group %q", g)
		}
	}
	if !mqttPrefix.MatchString(q.Prefix) {
		return invalid("discovery prefix")
	}
	if q.Port < 1 || q.Port > 65535 || len(q.Username) > 200 || len(q.Password) > 200 || strings.ContainsAny(q.Username+q.Password, "\r\n") {
		return invalid("MQTT port, user or password")
	}
	if q.Enabled && !mqttHost.MatchString(q.Host) {
		return invalid("MQTT broker address: host name or IP address")
	}
	return nil
}

func domainOf(entity string) string {
	d, _, _ := strings.Cut(entity, ".")
	return d
}

// Allowed says whether the assistant may see and use an entity.
func (m *Manager) Allowed(entity string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return allowed(m.settings, entity)
}

func allowed(s Settings, entity string) bool {
	if !ValidEntity(entity) {
		return false
	}
	if _, known := domainLevels[domainOf(entity)]; !known {
		return false
	}
	return slices.Contains(s.Domains, domainOf(entity)) || slices.Contains(s.Entities, entity)
}

// newClient talks to exactly the configured server: no redirects, and with
// a fingerprint only to the certificate it names.
func newClient(s Settings) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if s.Fingerprint != "" {
		want := s.Fingerprint
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, // replaced by the check of the fingerprint below
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				if len(raw) == 0 {
					return errors.New("no certificate")
				}
				sum := sha256.Sum256(raw[0])
				if hex.EncodeToString(sum[:]) != want {
					return errors.New("the certificate does not match the stored fingerprint")
				}
				return nil
			},
		}
	}
	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("the Home Assistant address answered with a redirect; enter the final address")
		},
	}
}

// maxAnswer limits what is read from the server.
const maxAnswer = 8 << 20

// do sends one request to the REST API and decodes the answer into out.
func (m *Manager) do(ctx context.Context, method, path string, body, out any) error {
	m.mu.Lock()
	s, client := m.settings, m.client
	m.mu.Unlock()
	if !s.Enabled || s.URL == "" || s.Token == "" {
		return ErrOff
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.URL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer+1))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if len(data) > maxAnswer {
		return fmt.Errorf("%w: answer too large", ErrUnreachable)
	}
	if res.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: the token was refused (401)", ErrUnreachable)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		detail := strings.TrimSpace(string(data))
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return fmt.Errorf("%w: %s %s", ErrUnreachable, res.Status, detail)
	}
	if out == nil {
		return nil
	}
	if text, ok := out.(*string); ok {
		*text = string(data)
		return nil
	}
	return json.Unmarshal(data, out)
}

// Status is what Check reports about the server.
type Status struct {
	Version  string `json:"version"`
	Location string `json:"location"`
}

// Check asks the server who it is.
func (m *Manager) Check(ctx context.Context) (Status, error) {
	var info struct {
		Version      string `json:"version"`
		LocationName string `json:"location_name"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/config", nil, &info); err != nil {
		return Status{}, err
	}
	return Status{Version: info.Version, Location: info.LocationName}, nil
}

// State is one entity as Home Assistant reports it.
type State struct {
	EntityID    string         `json:"entity_id"`
	State       string         `json:"state"`
	Attributes  map[string]any `json:"attributes"`
	LastChanged time.Time      `json:"last_changed"`
	Area        string         `json:"area,omitempty"`
}

// Name is the friendly name of an entity, or its ID.
func (s State) Name() string {
	if name, ok := s.Attributes["friendly_name"].(string); ok && name != "" {
		return name
	}
	return s.EntityID
}

// States returns all entities with their area; allowedOnly leaves out what
// was not released to the assistant.
func (m *Manager) States(ctx context.Context, allowedOnly bool) ([]State, error) {
	var states []State
	if err := m.do(ctx, http.MethodGet, "/api/states", nil, &states); err != nil {
		return nil, err
	}
	m.mu.Lock()
	settings := m.settings
	m.mu.Unlock()
	if allowedOnly {
		states = slices.DeleteFunc(states, func(s State) bool { return !allowed(settings, s.EntityID) })
	}
	if areas, err := m.areas(ctx); err == nil {
		for i := range states {
			states[i].Area = areas[states[i].EntityID]
		}
	}
	sort.Slice(states, func(i, j int) bool { return states[i].EntityID < states[j].EntityID })
	return states, nil
}

// areas maps entities to the names of their areas, through a template:
// the REST API has no other way to tell.
func (m *Manager) areas(ctx context.Context) (map[string]string, error) {
	var text string
	tmpl := `{% for s in states %}{{ s.entity_id }}	{{ area_name(s.entity_id) or '' }}
{% endfor %}`
	if err := m.do(ctx, http.MethodPost, "/api/template", map[string]string{"template": tmpl}, &text); err != nil {
		return nil, err
	}
	areas := map[string]string{}
	for line := range strings.SplitSeq(text, "\n") {
		entity, area, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && area != "" && area != "None" {
			areas[entity] = area
		}
	}
	return areas, nil
}

// State returns one entity.
func (m *Manager) State(ctx context.Context, entity string) (State, error) {
	if !ValidEntity(entity) {
		return State{}, invalid("entity")
	}
	var state State
	err := m.do(ctx, http.MethodGet, "/api/states/"+entity, nil, &state)
	return state, err
}

// NotifyServices returns the notify services, e.g. notify.mobile_app_pixel.
func (m *Manager) NotifyServices(ctx context.Context) ([]string, error) {
	var domains []struct {
		Domain   string                     `json:"domain"`
		Services map[string]json.RawMessage `json:"services"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/services", nil, &domains); err != nil {
		return nil, err
	}
	var list []string
	for _, d := range domains {
		if d.Domain != "notify" {
			continue
		}
		for name := range d.Services {
			list = append(list, "notify."+name)
		}
	}
	sort.Strings(list)
	return list, nil
}

// Call calls a service for released entities. Every entity has to be
// released and belong to the domain of the service.
func (m *Manager) Call(ctx context.Context, domain, service string, entities []string, data map[string]any) ([]State, error) {
	if _, ok := Level(domain); !ok || !ValidService(service) {
		return nil, ErrNotAllowed
	}
	if len(entities) == 0 || len(entities) > 50 {
		return nil, invalid("name 1 to 50 entities")
	}
	m.mu.Lock()
	settings := m.settings
	m.mu.Unlock()
	for _, e := range entities {
		if domainOf(e) != domain || !callAllowed(settings, e, service) {
			return nil, fmt.Errorf("%w: %s", ErrNotAllowed, e)
		}
	}
	body := map[string]any{}
	for k, v := range data {
		if k == "entity_id" || k == "device_id" || k == "area_id" || k == "floor_id" || k == "label_id" {
			return nil, invalid("data must not name targets; use entities")
		}
		body[k] = v
	}
	if b, _ := json.Marshal(body); len(b) > 4096 {
		return nil, invalid("data too large")
	}
	body["entity_id"] = entities
	var changed []State
	err := m.do(ctx, http.MethodPost, "/api/services/"+domain+"/"+service, body, &changed)
	return changed, err
}

// Notify sends a message through a notify service such as
// notify.mobile_app_pixel. data is passed on (actions, priority, url).
func (m *Manager) Notify(ctx context.Context, service, title, message string, data map[string]any) error {
	domain, name, ok := strings.Cut(service, ".")
	if !ok || domain != "notify" || !ValidService(name) {
		return invalid("notify service")
	}
	body := map[string]any{"title": title, "message": message}
	if len(data) > 0 {
		body["data"] = data
	}
	return m.do(ctx, http.MethodPost, "/api/services/notify/"+name, body, nil)
}
