package nvr

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// The recipients of a camera (PLAN 82-3): accounts an administrator chose
// at the camera, each with its ways. Who may get a message is decided
// twice: when the camera is saved (checkRecipients refuses what an
// account may not get) and again for every message (targets), so that an
// account that was locked, lost its right on the camera or was deleted
// and made anew under the same name gets nothing.

// Errors about the recipients of a camera; the API turns them into codes.
var (
	// ErrRecipientUnknown: the account does not exist or is locked.
	ErrRecipientUnknown = errors.New("nvr: a recipient does not exist or is locked")
	// ErrRecipientNoRight: the account may not see the camera.
	ErrRecipientNoRight = errors.New("nvr: a recipient may not see the camera")
	// ErrRecipientBell: the bell for an account that is no owner or
	// administrator (only they have one).
	ErrRecipientBell = errors.New("nvr: only owners and administrators have a bell")
)

// chosenBefore tells whether an entry chosen at added belongs to an
// earlier account of the same name: the account was made after it. An
// entry without a time belongs to no account that has one.
func chosenBefore(account Account, added time.Time) bool {
	return !account.Created.IsZero() && (added.IsZero() || account.Created.After(added))
}

// within tells whether r has no way that was does not have.
func within(r, was Recipient) bool {
	return (!r.Bell || was.Bell) && (!r.Push || was.Push) && (!r.Email || was.Email)
}

// checkRecipients checks the recipients of a camera that is being stored
// (with its ID) against the accounts as they are now; was is the camera
// as stored before, nil for a new one. An account added, or given a way it
// did not have, must exist, not be locked, may see the camera (owners and
// administrators always; others by their tick, which a camera that is new
// has for nobody) and gets the bell only as an owner or administrator.
// An entry as stored before, or with fewer ways, stays without that check:
// otherwise switching a camera off or reordering it would be refused once
// a recipient lost its right; such a recipient gets nothing anyway. An
// unchanged entry of an earlier account of the same name is dropped.
// Added is set here, never taken from the request.
func (m *Manager) checkRecipients(c Camera, was *Camera) (Camera, error) {
	n := &c.Notify
	if n.Recipients == nil {
		return c, nil
	}
	var before []Recipient
	if was != nil {
		before = was.Notify.Recipients
	}
	now := time.Now().UTC()
	out := make([]Recipient, 0, len(n.Recipients))
	for _, r := range n.Recipients {
		account, exists := m.Access.lookup(r.User)
		stored, kept := Recipient{}, false
		if i := slices.IndexFunc(before, func(b Recipient) bool { return b.User == r.User }); i >= 0 {
			stored, kept = before[i], true
		}
		earlier := kept && exists && chosenBefore(account, stored.Added)
		if exists && !account.Admin && r.Bell && kept && stored.Bell {
			// Chosen with the bell as an administrator, since then no
			// longer one: the bell goes, the rest stays. The form shows
			// such an account without a bell and could not remove it.
			r.Bell = false
		}
		if kept && within(r, stored) {
			if !earlier {
				r.Added = stored.Added
				out = append(out, r)
			}
			continue
		}
		switch {
		case !exists || account.Disabled:
			return c, fmt.Errorf("%w: %s", ErrRecipientUnknown, r.User)
		case !account.Admin && (!m.Access.Readable() || !m.Access.Sees(r.User, c.ID)):
			return c, fmt.Errorf("%w: %s", ErrRecipientNoRight, r.User)
		case r.Bell && !account.Admin:
			return c, fmt.Errorf("%w: %s", ErrRecipientBell, r.User)
		}
		r.Added = now
		if kept && !earlier {
			r.Added = stored.Added
		}
		out = append(out, r)
	}
	// An entry without any way is no recipient: it would only count in the
	// overview ("bob ()"). A demotion can take its last way, the bell.
	n.Recipients = slices.DeleteFunc(out, func(r Recipient) bool { return !r.Bell && !r.Push && !r.Email })
	n.keepWayBack()
	return c, nil
}

// keepWayBack sets the ways of 0.78 to "some recipient has this way", for
// a way back to 0.78 (validateNotify does the same).
func (n *Notify) keepWayBack() {
	if n.Recipients == nil {
		return
	}
	n.Titan, n.Push, n.Email = false, false, false
	for _, r := range n.Recipients {
		n.Titan, n.Push, n.Email = n.Titan || r.Bell, n.Push || r.Push, n.Email || r.Email
	}
}

// targets are the recipients of a camera that may get a message of kind
// now, with the ways they may have: the account exists, is not locked, is
// the one that was chosen (not made later under the same name), and may
// see the camera (owners and administrators always; others by their tick,
// none while the rights could not be read). Plates and faces name what
// the NVR recognised: they go to owners and administrators and to
// accounts with the right "names" on the camera, as only they see names
// and plates in the NVR (PLAN 82-3). The bell only to owners and
// administrators, as only they have one.
func (m *Manager) targets(c Camera, kind string) []Target {
	names := kind == KindPlate || kind == KindFace || kind == KindFaceUnknown
	out := []Target{}
	for _, r := range c.Notify.Recipients {
		account, ok := m.Access.lookup(r.User)
		if !ok || account.Disabled || chosenBefore(account, r.Added) {
			continue
		}
		if !account.Admin && (!m.Access.Readable() || !m.Access.Sees(r.User, c.ID) ||
			names && !m.Access.Allows(r.User, c.ID, RightNames)) {
			continue
		}
		t := Target{User: r.User, Bell: r.Bell && account.Admin, Push: r.Push, Email: r.Email}
		if t.Bell || t.Push || t.Email {
			out = append(out, t)
		}
	}
	return out
}

// ForgetRecipient takes a deleted account off every camera, so that an
// account made later under the same name does not find itself chosen in
// the camera's form. (Were this write to fail, Added still keeps the
// messages from it.)
func (m *Manager) ForgetRecipient(user string) error {
	m.camerasMu.Lock()
	defer m.camerasMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	list := cloneCameras(m.cameras)
	changed := false
	for i := range list {
		n := &list[i].Notify
		if n.Recipients == nil {
			continue
		}
		before := len(n.Recipients)
		n.Recipients = slices.DeleteFunc(n.Recipients, func(r Recipient) bool { return r.User == user })
		if len(n.Recipients) != before {
			n.keepWayBack()
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return m.storeCamerasLocked(list)
}
