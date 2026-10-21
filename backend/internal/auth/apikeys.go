package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// API keys let a program, such as an AI agent, use the MCP endpoint
// without a password, a cookie or a second factor, and only with the
// scopes it was given. See docs/AGENT-API.md.

const (
	apiSettingsFile = "system/api.json"
	// APIKeyPrefix starts every key, so that secret scanners can find a
	// key that ended up somewhere it should not be.
	APIKeyPrefix = "titanos_ak_"
	// MaxAPIKeys is how many keys one account may have.
	MaxAPIKeys = 20
)

func apiKeysFile(user string) string {
	return "users/" + user + "/api-keys.json"
}

// The scopes a key may have. Each opens a group of read-only tools.
const (
	ScopeStatus  = "status"
	ScopeStorage = "storage"
	ScopeDocker  = "docker"
	ScopeVMs     = "vms"
	ScopeBackups = "backups"
	ScopeLogs    = "logs"
)

// Scopes lists every scope in the order the web UI shows them.
var Scopes = []string{ScopeStatus, ScopeStorage, ScopeDocker, ScopeVMs, ScopeBackups, ScopeLogs}

var (
	ErrAPIOff         = errors.New("auth: API keys are switched off")
	ErrInvalidAPIKey  = errors.New("auth: unknown or wrong API key")
	ErrKeyNotAllowed  = errors.New("auth: this account may not have API keys")
	ErrTooManyAPIKeys = errors.New("auth: too many API keys")
	ErrInvalidKeyName = errors.New("auth: invalid key name")
	ErrInvalidScope   = errors.New("auth: invalid scope")
	ErrInvalidNetwork = errors.New("auth: invalid network")
	ErrInvalidExpiry  = errors.New("auth: invalid expiry")
)

// APIKey is a key as stored in /config/users/<name>/api-keys.json. Only a
// hash of its secret is kept.
type APIKey struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Hash     string     `json:"hash"`
	Scopes   []string   `json:"scopes"`
	Created  time.Time  `json:"created"`
	Expires  *time.Time `json:"expires,omitempty"`
	Networks []string   `json:"networks,omitempty"`
}

// Has reports whether the key was given a scope.
func (k APIKey) Has(scope string) bool { return slices.Contains(k.Scopes, scope) }

type apiKeyFile struct {
	Keys []APIKey `json:"keys"`
}

// APIKeyInfo is a key as the web UI shows it.
type APIKeyInfo struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Scopes   []string   `json:"scopes"`
	Created  time.Time  `json:"created"`
	Expires  *time.Time `json:"expires"`
	Expired  bool       `json:"expired"`
	Networks []string   `json:"networks"`
	// LastUsed and LastIP are only known since titand started: they are
	// kept in memory, so that using a key never writes to the stick.
	LastUsed *time.Time `json:"lastUsed"`
	LastIP   string     `json:"lastIP"`
}

// NewAPIKey is what the web UI asks for.
type NewAPIKey struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
	// ExpiresDays is 0 for a key that does not expire.
	ExpiresDays int      `json:"expiresDays"`
	Networks    []string `json:"networks"`
}

type keyUse struct {
	at time.Time
	ip string
}

// APIKeys holds the keys of all accounts and the switch that allows them.
type APIKeys struct {
	store *config.Store
	users *Users
	now   func() time.Time

	mu      sync.Mutex
	loaded  bool
	enabled bool
	keys    map[string][]APIKey // by account
	used    map[string]keyUse   // by key ID
}

// NewAPIKeys returns the keys in store. Call Load after the accounts are loaded.
func NewAPIKeys(store *config.Store, users *Users) *APIKeys {
	return &APIKeys{store: store, users: users, now: time.Now, keys: map[string][]APIKey{}, used: map[string]keyUse{}}
}

// Load reads the switch and the keys of every account.
func (a *APIKeys) Load() error {
	var settings struct {
		Enabled bool `json:"enabled"`
	}
	if err := a.store.ReadJSON(apiSettingsFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	keys := map[string][]APIKey{}
	for _, user := range a.users.List() {
		var file apiKeyFile
		err := a.store.ReadJSON(apiKeysFile(user.Name), &file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("API keys of %s: %w", user.Name, err)
		}
		keys[user.Name] = file.Keys
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enabled, a.keys, a.loaded = settings.Enabled, keys, true
	return nil
}

// Loaded reports whether Load has worked.
func (a *APIKeys) Loaded() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loaded
}

// Enabled reports whether API keys may be used.
func (a *APIKeys) Enabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.enabled
}

// SetEnabled allows or forbids API keys. Keys are kept either way.
func (a *APIKeys) SetEnabled(enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.store.WriteJSON(apiSettingsFile, map[string]bool{"enabled": enabled}); err != nil {
		return err
	}
	a.enabled = enabled
	return nil
}

// MayHaveKeys reports whether an account of this role may hold keys: the
// tools show what only administrators see in the web UI.
func MayHaveKeys(role Role) bool { return role.CanAdminister() }

// List returns the keys of an account.
func (a *APIKeys) List(user string) []APIKeyInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	list := []APIKeyInfo{}
	for _, key := range a.keys[user] {
		info := APIKeyInfo{ID: key.ID, Name: key.Name, Scopes: key.Scopes, Created: key.Created, Expires: key.Expires, Networks: key.Networks}
		if info.Networks == nil {
			info.Networks = []string{}
		}
		info.Expired = key.Expires != nil && !now.Before(*key.Expires)
		if use, ok := a.used[key.ID]; ok {
			at := use.at
			info.LastUsed, info.LastIP = &at, use.ip
		}
		list = append(list, info)
	}
	return list
}

// Count returns how many keys an account has.
func (a *APIKeys) Count(user string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.keys[user])
}

func validKeyName(name string) bool {
	if name == "" || len(name) > 40 || strings.TrimSpace(name) != name {
		return false
	}
	for _, c := range name {
		if c < ' ' || c == 0x7f {
			return false
		}
	}
	return true
}

func normalizeNetworks(networks []string) ([]string, error) {
	var list []string
	for _, network := range networks {
		network = strings.TrimSpace(network)
		if network == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(network)
		if err != nil {
			// A single address is a network of one.
			addr, addrErr := netip.ParseAddr(network)
			if addrErr != nil {
				return nil, ErrInvalidNetwork
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		list = append(list, prefix.Masked().String())
	}
	if len(list) > 16 {
		return nil, ErrInvalidNetwork
	}
	return list, nil
}

func randomString(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Create makes a key for an account and returns it once, in full. Only
// its hash is stored.
func (a *APIKeys) Create(user string, req NewAPIKey) (string, APIKeyInfo, error) {
	account, ok := a.users.Get(user)
	if !ok {
		return "", APIKeyInfo{}, ErrNotFound
	}
	if !MayHaveKeys(account.Role) {
		return "", APIKeyInfo{}, ErrKeyNotAllowed
	}
	if !validKeyName(req.Name) {
		return "", APIKeyInfo{}, ErrInvalidKeyName
	}
	var scopes []string
	for _, scope := range Scopes {
		if slices.Contains(req.Scopes, scope) {
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) == 0 || len(scopes) != len(slices.Compact(slices.Sorted(slices.Values(req.Scopes)))) {
		return "", APIKeyInfo{}, ErrInvalidScope
	}
	if req.ExpiresDays < 0 || req.ExpiresDays > 3650 {
		return "", APIKeyInfo{}, ErrInvalidExpiry
	}
	networks, err := normalizeNetworks(req.Networks)
	if err != nil {
		return "", APIKeyInfo{}, err
	}

	// The ID is hex: it never contains the "_" that separates it from the secret.
	idBytes := make([]byte, 4)
	if _, err := rand.Read(idBytes); err != nil {
		return "", APIKeyInfo{}, err
	}
	id := hex.EncodeToString(idBytes)
	secret, err := randomString(32)
	if err != nil {
		return "", APIKeyInfo{}, err
	}
	sum := sha256.Sum256([]byte(secret))
	now := a.now().UTC().Truncate(time.Second)
	key := APIKey{ID: id, Name: req.Name, Hash: hex.EncodeToString(sum[:]), Scopes: scopes, Created: now, Networks: networks}
	if req.ExpiresDays > 0 {
		expires := now.AddDate(0, 0, req.ExpiresDays)
		key.Expires = &expires
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	keys := a.keys[user]
	if len(keys) >= MaxAPIKeys {
		return "", APIKeyInfo{}, ErrTooManyAPIKeys
	}
	for _, other := range keys {
		if strings.EqualFold(other.Name, key.Name) {
			return "", APIKeyInfo{}, ErrInvalidKeyName
		}
	}
	keys = append(slices.Clone(keys), key)
	if err := a.store.WriteJSON(apiKeysFile(user), apiKeyFile{Keys: keys}); err != nil {
		return "", APIKeyInfo{}, err
	}
	a.keys[user] = keys
	info := APIKeyInfo{ID: id, Name: key.Name, Scopes: scopes, Created: now, Expires: key.Expires, Networks: networks}
	if info.Networks == nil {
		info.Networks = []string{}
	}
	return APIKeyPrefix + id + "_" + secret, info, nil
}

// Remove deletes a key of an account; it stops working at once.
func (a *APIKeys) Remove(user, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	keys := a.keys[user]
	i := slices.IndexFunc(keys, func(key APIKey) bool { return key.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	keys = slices.Delete(slices.Clone(keys), i, i+1)
	var err error
	if len(keys) == 0 {
		err = a.store.Remove(apiKeysFile(user))
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	} else {
		err = a.store.WriteJSON(apiKeysFile(user), apiKeyFile{Keys: keys})
	}
	if err != nil {
		return err
	}
	a.keys[user] = keys
	delete(a.used, id)
	return nil
}

// RemoveAll deletes every key of an account.
func (a *APIKeys) RemoveAll(user string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.store.Remove(apiKeysFile(user)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	a.forget(user)
	return nil
}

// Forget drops the keys of an account that was deleted together with its folder.
func (a *APIKeys) Forget(user string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.forget(user)
}

func (a *APIKeys) forget(user string) {
	for _, key := range a.keys[user] {
		delete(a.used, key.ID)
	}
	delete(a.keys, user)
}

// parseAPIKey splits titanos_ak_<id>_<secret>.
func parseAPIKey(token string) (id, secret string, ok bool) {
	rest, found := strings.CutPrefix(token, APIKeyPrefix)
	if !found {
		return "", "", false
	}
	id, secret, found = strings.Cut(rest, "_")
	if !found || len(id) != 8 || secret == "" || len(secret) > 100 {
		return "", "", false
	}
	return id, secret, true
}

func allowedFrom(networks []string, ip string) bool {
	if len(networks) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, network := range networks {
		if prefix, err := netip.ParsePrefix(network); err == nil && prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// Authenticate returns the account and the key behind a token sent from
// ip. A key whose account lost the right to keys, that expired or that is
// used from a network it is not allowed in is as good as a wrong one.
func (a *APIKeys) Authenticate(token, ip string) (User, APIKey, error) {
	id, secret, ok := parseAPIKey(token)
	a.mu.Lock()
	enabled := a.enabled
	var owner string
	var key APIKey
	found := false
	if ok {
		for user, keys := range a.keys {
			if i := slices.IndexFunc(keys, func(key APIKey) bool { return key.ID == id }); i >= 0 {
				owner, key, found = user, keys[i], true
				break
			}
		}
	}
	a.mu.Unlock()
	if !enabled {
		return User{}, APIKey{}, ErrAPIOff
	}
	if !found {
		return User{}, APIKey{}, ErrInvalidAPIKey
	}
	sum := sha256.Sum256([]byte(secret))
	want, err := hex.DecodeString(key.Hash)
	if err != nil || subtle.ConstantTimeCompare(sum[:], want) != 1 {
		return User{}, APIKey{}, ErrInvalidAPIKey
	}
	now := a.now()
	if key.Expires != nil && !now.Before(*key.Expires) || !allowedFrom(key.Networks, ip) {
		return User{}, APIKey{}, ErrInvalidAPIKey
	}
	// Deleting an account forgets its keys (Forget), so a new account of
	// the same name never finds old ones.
	user, exists := a.users.Get(owner)
	if !exists || !MayHaveKeys(user.Role) {
		return User{}, APIKey{}, ErrInvalidAPIKey
	}
	a.mu.Lock()
	a.used[key.ID] = keyUse{at: now, ip: ip}
	a.mu.Unlock()
	return user, key, nil
}
