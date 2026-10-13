package auth

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// Role decides what an account may do.
type Role string

const (
	RoleOwner Role = "owner" // everything; at least one must exist
	RoleAdmin Role = "admin" // everything except managing owners
	RoleUser  Role = "user"  // own shares and files only
)

// CanAdminister reports whether the role may change the system.
func (r Role) CanAdminister() bool {
	return r == RoleOwner || r == RoleAdmin
}

// User is an account as stored in /config/users/<name>/account.json.
type User struct {
	Name         string    `json:"name"`
	Role         Role      `json:"role"`
	PasswordHash string    `json:"passwordHash"`
	Created      time.Time `json:"created"`
}

var (
	ErrInvalidName      = errors.New("auth: invalid account name")
	ErrReservedName     = errors.New("auth: account name is reserved for the system")
	ErrPasswordTooShort = errors.New("auth: password too short")
	ErrPasswordTooLong  = errors.New("auth: password too long")
	ErrExists           = errors.New("auth: account exists")
	ErrSetupDone        = errors.New("auth: an owner account already exists")
	ErrNotLoaded        = errors.New("auth: accounts could not be read from /config")
)

// Account names double as Linux and SMB user names.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Names of system accounts in the image; an account with one of these names
// could never get its own Linux user.
var reservedNames = []string{
	"root", "daemon", "bin", "sys", "sync", "games", "man", "lp", "mail",
	"news", "uucp", "proxy", "www-data", "backup", "list", "irc", "nobody",
	"messagebus", "sshd", "docker", "libvirt", "libvirt-qemu", "kvm",
	"users", "staff", "sudo", "adm", "shadow", "disk", "titand", "titanos",
}

// ValidateName checks an account name without creating anything.
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return ErrInvalidName
	}
	if slices.Contains(reservedNames, name) || strings.HasPrefix(name, "systemd-") {
		return ErrReservedName
	}
	return nil
}

// ValidatePassword checks the length limits.
func ValidatePassword(password string) error {
	switch n := len([]rune(password)); {
	case n < MinPasswordLength:
		return ErrPasswordTooShort
	case len(password) > MaxPasswordLength:
		return ErrPasswordTooLong
	}
	return nil
}

// Users is the set of accounts. It is read from the store once and then
// kept in memory, so that logins do not wake up the stick.
type Users struct {
	store *config.Store

	mu     sync.RWMutex
	users  map[string]User
	loaded bool
}

// NewUsers returns the accounts in store. Call Load before using them.
func NewUsers(store *config.Store) *Users {
	return &Users{store: store, users: map[string]User{}}
}

func accountFile(name string) string {
	return "users/" + name + "/account.json"
}

// Load reads all accounts from the store.
func (u *Users) Load() error {
	names, err := u.store.List("users")
	if err != nil {
		return err
	}
	users := make(map[string]User, len(names))
	for _, name := range names {
		if ValidateName(name) != nil {
			continue // not an account directory
		}
		var user User
		if err := u.store.ReadJSON(accountFile(name), &user); err != nil {
			return fmt.Errorf("account %s: %w", name, err)
		}
		if user.Name != name {
			return fmt.Errorf("account %s: file belongs to %q", name, user.Name)
		}
		users[name] = user
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	u.users = users
	u.loaded = true
	return nil
}

// Loaded reports whether the accounts were read successfully. While they
// were not, nobody can log in and no account can be created.
func (u *Users) Loaded() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.loaded
}

// Count returns the number of accounts.
func (u *Users) Count() int {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return len(u.users)
}

// Get returns the account with the given name.
func (u *Users) Get(name string) (User, bool) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	user, ok := u.users[name]
	return user, ok
}

// List returns all accounts sorted by name.
func (u *Users) List() []User {
	u.mu.RLock()
	defer u.mu.RUnlock()
	list := make([]User, 0, len(u.users))
	for _, user := range u.users {
		list = append(list, user)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// CreateOwner creates the first account. It fails with ErrSetupDone as soon
// as any account exists.
func (u *Users) CreateOwner(name, password string) (User, error) {
	return u.create(name, password, RoleOwner, true)
}

// Create adds an account.
func (u *Users) Create(name, password string, role Role) (User, error) {
	return u.create(name, password, role, false)
}

func (u *Users) create(name, password string, role Role, first bool) (User, error) {
	if err := ValidateName(name); err != nil {
		return User{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.loaded {
		return User{}, ErrNotLoaded
	}
	if first && len(u.users) > 0 {
		return User{}, ErrSetupDone
	}
	if _, exists := u.users[name]; exists {
		return User{}, ErrExists
	}

	user := User{Name: name, Role: role, PasswordHash: hash, Created: time.Now().UTC().Truncate(time.Second)}
	if err := u.store.WriteJSON(accountFile(name), user); err != nil {
		return User{}, err
	}
	u.users[name] = user
	return user, nil
}

// dummyHash is verified when the account does not exist, so that a login
// attempt takes the same time either way and does not reveal which names exist.
var dummyHash = sync.OnceValue(func() string {
	hash, err := hashPassword("titanos-dummy-password")
	if err != nil {
		panic(err)
	}
	return hash
})

// Authenticate checks a name and password.
func (u *Users) Authenticate(name, password string) (User, bool) {
	user, exists := u.Get(name)
	if len(password) > MaxPasswordLength {
		return User{}, false
	}
	hash := user.PasswordHash
	if !exists {
		hash = dummyHash()
	}
	ok, err := verifyPassword(hash, password)
	if err != nil || !ok || !exists {
		return User{}, false
	}
	return user, true
}
