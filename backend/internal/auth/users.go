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
	// UID is the number of the Linux user with the same name. The system is
	// rebuilt on every boot, so the number has to be kept here for files to
	// keep their owner.
	UID int `json:"uid"`
	// SMBHash is the NT hash Samba needs to check the password. It is a
	// weak, unsalted hash by today's standards, but the SMB protocol works
	// with nothing else.
	SMBHash string `json:"smbHash,omitempty"`
	// TOTPSecret is the secret shared with the authenticator app; empty
	// while two-factor login is off. RecoveryCodes are the hashes of the
	// one-time codes for a lost phone.
	TOTPSecret    string   `json:"totpSecret,omitempty"`
	RecoveryCodes []string `json:"recoveryCodes,omitempty"`
	// UsedTOTPStep prevents replay between the daemon, local recovery and
	// subsequent daemon starts. It is saved with the accepted factor.
	UsedTOTPStep uint64 `json:"usedTOTPStep,omitempty"`
	// SSHKeys are the public keys the account may log in with over SSH,
	// one line each as in an authorized_keys file.
	SSHKeys []string `json:"sshKeys,omitempty"`
}

// TwoFactor reports whether the account logs in with a second factor.
func (u User) TwoFactor() bool { return u.TOTPSecret != "" }

// firstUID is the lowest number given to an account's Linux user. It stays
// clear of the system users and of the usual first user (1000) on other
// systems, whose files might be copied onto the NAS.
const firstUID = 2000

var (
	ErrInvalidName      = errors.New("auth: invalid account name")
	ErrReservedName     = errors.New("auth: account name is reserved for the system")
	ErrPasswordTooShort = errors.New("auth: password too short")
	ErrPasswordTooLong  = errors.New("auth: password too long")
	ErrExists           = errors.New("auth: account exists")
	ErrSetupDone        = errors.New("auth: an owner account already exists")
	ErrNotLoaded        = errors.New("auth: accounts could not be read from /config")
	ErrNotFound         = errors.New("auth: no such account")
	ErrInvalidRole      = errors.New("auth: unknown role")
	ErrLastOwner        = errors.New("auth: the last owner cannot be removed or demoted")
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

// Users caches ordinary password checks. Mutations and second-factor
// consumption reload under a cross-process lock; no access runs on a timer.
type Users struct {
	store *config.Store

	mu     sync.RWMutex
	users  map[string]User
	loaded bool
	// pendingTOTP holds secrets that were handed out but not confirmed yet.
	pendingTOTP map[string]string
}

// NewUsers returns the accounts in store. Call Load before using them.
func NewUsers(store *config.Store) *Users {
	return &Users{store: store, users: map[string]User{}, pendingTOTP: map[string]string{}}
}

func accountFile(name string) string {
	return "users/" + name + "/account.json"
}

// Load reads all accounts from the store.
func (u *Users) Load() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	unlock, err := u.lockAccountFiles()
	if err != nil {
		return err
	}
	defer unlock()
	if err := u.reloadAccountsLocked(); err != nil {
		return err
	}
	u.loaded = true
	return nil
}

// reloadAccountsLocked requires mu and the account-file lock.
func (u *Users) reloadAccountsLocked() error {
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

	// Accounts from before Linux users existed get their number now.
	for name, user := range users {
		if user.UID == 0 {
			user.UID = nextUID(users)
			if err := u.store.WriteJSON(accountFile(name), user); err != nil {
				return fmt.Errorf("account %s: %w", name, err)
			}
			users[name] = user
		}
	}

	u.users = users
	return nil
}

func nextUID(users map[string]User) int {
	uid := firstUID
	for _, user := range users {
		uid = max(uid, user.UID+1)
	}
	return uid
}

func validRole(role Role) bool {
	return role == RoleOwner || role == RoleAdmin || role == RoleUser
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
	if !validRole(role) {
		return User{}, ErrInvalidRole
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
	unlock, err := u.lockAccountFiles()
	if err != nil {
		return User{}, err
	}
	defer unlock()
	if err := u.reloadAccountsLocked(); err != nil {
		return User{}, err
	}
	if first && len(u.users) > 0 {
		return User{}, ErrSetupDone
	}
	if _, exists := u.users[name]; exists {
		return User{}, ErrExists
	}

	user := User{
		Name: name, Role: role, PasswordHash: hash, Created: time.Now().UTC().Truncate(time.Second),
		UID: nextUID(u.users), SMBHash: ntHash(password),
	}
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

// update changes one account and writes it to the store.
func (u *Users) update(name string, change func(user *User) error) (User, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.loaded {
		return User{}, ErrNotLoaded
	}
	unlock, err := u.lockAccountFiles()
	if err != nil {
		return User{}, err
	}
	defer unlock()
	if err := u.reloadAccountsLocked(); err != nil {
		return User{}, err
	}
	user, ok := u.users[name]
	if !ok {
		return User{}, ErrNotFound
	}
	if err := change(&user); err != nil {
		return User{}, err
	}
	if err := u.store.WriteJSON(accountFile(name), user); err != nil {
		return User{}, err
	}
	u.users[name] = user
	return user, nil
}

// owners counts the accounts with the owner role. The caller holds the lock.
func (u *Users) owners() int {
	count := 0
	for _, user := range u.users {
		if user.Role == RoleOwner {
			count++
		}
	}
	return count
}

// SetPassword gives an account a new password.
func (u *Users) SetPassword(name, password string) (User, error) {
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	return u.update(name, func(user *User) error {
		user.PasswordHash = hash
		user.SMBHash = ntHash(password)
		return nil
	})
}

// RememberSMBHash stores the hash Samba needs for an account that does not
// have one yet (created before shares existed). It is called at login, the
// only moment the password is known.
func (u *Users) RememberSMBHash(name, password string) (User, bool) {
	if user, ok := u.Get(name); !ok || user.SMBHash != "" {
		return user, false
	}
	user, err := u.update(name, func(user *User) error {
		user.SMBHash = ntHash(password)
		return nil
	})
	return user, err == nil
}

// SetRole changes what an account may do. One owner always remains.
func (u *Users) SetRole(name string, role Role) (User, error) {
	if !validRole(role) {
		return User{}, ErrInvalidRole
	}
	return u.update(name, func(user *User) error {
		if user.Role == RoleOwner && role != RoleOwner && u.owners() == 1 {
			return ErrLastOwner
		}
		user.Role = role
		return nil
	})
}

// Delete removes an account and its settings. One owner always remains.
func (u *Users) Delete(name string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	unlock, err := u.lockAccountFiles()
	if err != nil {
		return err
	}
	defer unlock()
	if err := u.reloadAccountsLocked(); err != nil {
		return err
	}
	user, ok := u.users[name]
	if !ok {
		return ErrNotFound
	}
	if user.Role == RoleOwner && u.owners() == 1 {
		return ErrLastOwner
	}
	if err := u.store.RemoveAll("users/" + name); err != nil {
		return err
	}
	delete(u.users, name)
	return nil
}
