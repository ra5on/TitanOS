package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const tokenFile = "users/api-tokens.json"

var TokenScopes = []string{"system:read", "files:read", "files:write", "docker:read", "docker:write", "vms:read", "vms:write", "mcp"}
var ErrInvalidToken = errors.New("invalid token name, scopes or token limit")

type APIToken struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	User           string    `json:"user"`
	AccountCreated time.Time `json:"accountCreated"`
	AccountKey     string    `json:"accountKey,omitempty"`
	Scopes         []string  `json:"scopes"`
	Created        time.Time `json:"created"`
	LastUsed       time.Time `json:"lastUsed,omitempty"`
	Hash           string    `json:"hash,omitempty"`
}

// Tokens are loaded once; use never writes to or polls the boot stick.
type Tokens struct {
	mu     sync.Mutex
	store  *config.Store
	tokens []APIToken
	loaded bool
}

func NewTokens(store *config.Store) *Tokens {
	t := &Tokens{store: store}
	t.mu.Lock()
	t.loadLocked()
	t.mu.Unlock()
	return t
}

// loadLocked reads the tokens until that worked once. A missing file means "no tokens" only when the stick is there
// (its users folder can be listed): a stick that was not mounted yet at
// start must not lead to a new, empty file that revokes every token.
func (t *Tokens) loadLocked() {
	if t.loaded || t.store == nil {
		return
	}
	var tokens []APIToken
	err := t.store.ReadJSON(tokenFile, &tokens)
	if errors.Is(err, fs.ErrNotExist) {
		if names, listErr := t.store.List("users"); listErr == nil && names != nil {
			err = nil
		}
	}
	if err == nil {
		t.tokens, t.loaded = tokens, true
	}
}

func (t *Tokens) List(user string) []APIToken {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.loadLocked()
	list := []APIToken{}
	for _, token := range t.tokens {
		if token.User == user {
			token.Hash = ""
			token.AccountKey = ""
			token.Scopes = slices.Clone(token.Scopes)
			list = append(list, token)
		}
	}
	return list
}

func (t *Tokens) Create(user User, name string, scopes []string) (APIToken, string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.loadLocked()
	if !t.loaded {
		return APIToken{}, "", ErrNotLoaded
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 || strings.ContainsAny(name, "\r\n\x00") {
		return APIToken{}, "", ErrInvalidToken
	}
	if len(scopes) == 0 {
		scopes = []string{"system:read"}
	}
	scopes = slices.Compact(slices.Sorted(slices.Values(scopes)))
	for _, scope := range scopes {
		if !slices.Contains(TokenScopes, scope) || (!user.Role.CanAdminister() && (strings.HasPrefix(scope, "docker:") || strings.HasPrefix(scope, "vms:"))) {
			return APIToken{}, "", ErrInvalidToken
		}
	}
	count := 0
	for _, token := range t.tokens {
		if token.User == user.Name {
			count++
		}
	}
	if count >= 32 {
		return APIToken{}, "", ErrInvalidToken
	}
	idBytes := make([]byte, 12)
	secret := make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return APIToken{}, "", err
	}
	if _, err := rand.Read(secret); err != nil {
		return APIToken{}, "", err
	}
	id := hex.EncodeToString(idBytes)
	plain := "titanos_" + id + "_" + base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(plain))
	token := APIToken{ID: id, Name: name, User: user.Name, AccountCreated: user.Created, AccountKey: TokenAccountKey(user), Scopes: scopes, Created: time.Now().UTC(), Hash: hex.EncodeToString(hash[:])}
	next := append(slices.Clone(t.tokens), token)
	if err := t.store.WriteJSON(tokenFile, next); err != nil {
		return APIToken{}, "", err
	}
	t.tokens = next
	token.Hash = ""
	token.AccountKey = ""
	return token, plain, nil
}

// Password changes and delete/recreate invalidate tokens even when account
// creation timestamps have the same one-second resolution.
func TokenAccountKey(user User) string {
	h := sha256.Sum256([]byte(user.PasswordHash))
	return hex.EncodeToString(h[:])
}

func (t *Tokens) Lookup(plain string) (APIToken, bool) {
	if len(plain) > 128 {
		return APIToken{}, false
	}
	parts := strings.SplitN(plain, "_", 3)
	if len(parts) != 3 || parts[0] != "titanos" {
		return APIToken{}, false
	}
	hash := sha256.Sum256([]byte(plain))
	t.mu.Lock()
	defer t.mu.Unlock()
	t.loadLocked()
	if !t.loaded {
		return APIToken{}, false
	}
	for i := range t.tokens {
		token := &t.tokens[i]
		if token.ID != parts[1] {
			continue
		}
		expected, err := hex.DecodeString(token.Hash)
		if err != nil || subtle.ConstantTimeCompare(hash[:], expected) != 1 {
			return APIToken{}, false
		}
		token.LastUsed = time.Now().UTC()
		copy := *token
		copy.Hash = ""
		copy.Scopes = slices.Clone(copy.Scopes)
		return copy, true
	}
	return APIToken{}, false
}

func (t *Tokens) Revoke(user, id string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.loadLocked()
	if !t.loaded {
		return ErrNotLoaded
	}
	next := slices.DeleteFunc(slices.Clone(t.tokens), func(token APIToken) bool { return token.User == user && token.ID == id })
	if len(next) == len(t.tokens) {
		return ErrNotFound
	}
	if err := t.store.WriteJSON(tokenFile, next); err != nil {
		return err
	}
	t.tokens = next
	return nil
}
