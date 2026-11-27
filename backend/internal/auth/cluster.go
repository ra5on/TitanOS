package auth

import "crypto/rand"

// Accounts of a cluster: every node keeps the same accounts. The cluster
// package takes them over from the other nodes with Import and Forget; the
// Linux user number stays that of this node, since files on its volumes
// belong to it.

// Import writes user as it came from another node. An account of the same
// name is replaced, keeping its UID; a new one gets the next free number
// here. It returns the account as it was before, if there was one.
func (u *Users) Import(user User) (before User, existed bool, err error) {
	if err := ValidateName(user.Name); err != nil {
		return User{}, false, err
	}
	if !validRole(user.Role) {
		return User{}, false, ErrInvalidRole
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.loaded {
		return User{}, false, ErrNotLoaded
	}
	unlock, err := u.lockAccountFiles()
	if err != nil {
		return User{}, false, err
	}
	defer unlock()
	if err := u.reloadAccountsLocked(); err != nil {
		return User{}, false, err
	}
	before, existed = u.users[user.Name]
	if existed {
		user.UID = before.UID
		// Keep the authoritative peer revision exactly as received. A separate
		// local stamp prevents an imported counter (possibly zero or lower)
		// from resurrecting a grant made for this NAS's previous account.
		if user.LocalAccessStamp == "" {
			user.LocalAccessStamp = before.LocalAccessStamp
			if user.Role != before.Role || user.Disabled != before.Disabled || user.AccessRevision != before.AccessRevision {
				user.LocalAccessStamp = rand.Text()
			}
		}
	} else {
		user.UID = nextUID(u.users)
	}
	if err := u.store.WriteJSON(accountFile(user.Name), user); err != nil {
		return User{}, false, err
	}
	u.users[user.Name] = user
	return before, existed, nil
}

// Forget removes an account another node removed. Unlike Delete it keeps
// no owner: the node that removed it checked that already.
func (u *Users) Forget(name string) (before User, existed bool, err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.loaded {
		return User{}, false, ErrNotLoaded
	}
	unlock, err := u.lockAccountFiles()
	if err != nil {
		return User{}, false, err
	}
	defer unlock()
	if err := u.reloadAccountsLocked(); err != nil {
		return User{}, false, err
	}
	before, existed = u.users[name]
	if !existed {
		return User{}, false, nil
	}
	if err := u.store.RemoveAll("users/" + name); err != nil {
		return User{}, false, err
	}
	u.rememberGoneLocked(before)
	delete(u.users, name)
	return before, true, nil
}
