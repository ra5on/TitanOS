// Package personalbackup encrypts each account's selected PIM collections
// for the recipient chosen by that account. A restic operator sees only the
// age ciphertext; the identity used for recovery is never kept on the NAS.
package personalbackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"slices"
	"time"

	"filippo.io/age"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

type Settings struct {
	Created   time.Time             `json:"created"`
	Recipient string                `json:"recipient"`
	Sources   []store.ArchiveSource `json:"sources"`
}
type Manager struct {
	Config *config.Store
	PIM    *store.Store
}

func settingFile(account string) string { return "users/" + account + "/personal-backup.json" }
func (m Manager) Settings(account string) (Settings, error) {
	var result Settings
	if !store.ValidAccount(account) || m.PIM == nil || m.PIM.Account == nil {
		return result, store.ErrNoAccount
	}
	created, ok := m.PIM.Account(account)
	if !ok {
		return result, store.ErrNoAccount
	}
	err := m.Config.ReadJSON(settingFile(account), &result)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !created.Equal(result.Created) {
		return Settings{}, nil
	}
	return result, err
}
func (m Manager) Set(account string, s Settings) error {
	if !store.ValidAccount(account) || m.PIM == nil || m.PIM.Account == nil {
		return store.ErrNoAccount
	}
	created, ok := m.PIM.Account(account)
	if !ok {
		return store.ErrNoAccount
	}
	if len(s.Sources) == 0 {
		err := m.Config.Remove(settingFile(account))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if _, err := age.ParseX25519Recipient(s.Recipient); err != nil || len(s.Sources) > 3*store.MaxCollections {
		return store.ErrArchive
	}
	seen := map[store.ArchiveSource]bool{}
	for _, src := range s.Sources {
		if seen[src] {
			return store.ErrArchive
		}
		seen[src] = true
		if _, err := m.PIM.GetCollection(src.Kind, account, src.ID); err != nil {
			return err
		}
	}
	s.Created = created
	return m.Config.WriteJSON(settingFile(account), s)
}

// ID binds an operator's job to this account incarnation and recovery key.
func (s Settings) ID() string {
	if s.Recipient == "" || len(s.Sources) == 0 {
		return ""
	}
	hash := sha256.Sum256([]byte(s.Recipient + "\x00" + s.Created.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(hash[:16])
}

func (m Manager) Write(ctx context.Context, w io.Writer, account, id, localDestination string) error {
	if m.PIM == nil {
		return store.ErrUnavailable
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.PIM.Context(account), cancel)
	defer cancel()
	defer stop()
	s, err := m.Settings(account)
	if err != nil {
		return err
	}
	if id == "" || s.ID() != id {
		return store.ErrPrecondition
	}
	for _, source := range s.Sources {
		if localDestination != "" && m.PIM.Current != nil && m.PIM.Current(string(source.Kind)) == localDestination {
			return errors.New("the backup must go to another volume than the personal data")
		}
	}
	key, err := age.ParseX25519Recipient(s.Recipient)
	if err != nil {
		return err
	}
	out, err := age.Encrypt(w, key)
	if err != nil {
		return err
	}
	if err = m.PIM.WriteArchive(ctx, out, account, s.Sources); err != nil {
		return err
	}
	// Revoking the grant, changing the selection or rotating the key while
	// exporting must not produce a complete, usable backup at the target.
	current, err := m.Settings(account)
	if err != nil {
		return err
	}
	if current.ID() != id || !slices.Equal(s.Sources, current.Sources) {
		return store.ErrPrecondition
	}
	return out.Close()
}
