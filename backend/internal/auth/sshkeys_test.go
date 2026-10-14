package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"golang.org/x/crypto/ssh"
)

const testPassword = "correct horse battery"

func newTestUsers(t *testing.T) *Users {
	t.Helper()
	users := NewUsers(config.Open(t.TempDir()))
	if err := users.Load(); err != nil {
		t.Fatal(err)
	}
	return users
}

func testKey(t *testing.T, comment string) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " " + comment
}

func TestSSHKeys(t *testing.T) {
	users := newTestUsers(t)
	if _, err := users.CreateOwner("alex", testPassword); err != nil {
		t.Fatal(err)
	}
	first, second := testKey(t, "alex@laptop"), testKey(t, "alex@phone")
	user, err := users.AddSSHKey("alex", "  "+first+"  ")
	if err != nil {
		t.Fatal(err)
	}
	keys := user.DescribeSSHKeys()
	if len(keys) != 1 || keys[0].Type != "ssh-ed25519" || keys[0].Comment != "alex@laptop" || !strings.HasPrefix(keys[0].Fingerprint, "SHA256:") || len(keys[0].ID) != 16 {
		t.Fatalf("keys: %+v", keys)
	}
	if user.SSHKeys[0] != first {
		t.Errorf("stored as %q", user.SSHKeys[0])
	}
	if _, err := users.AddSSHKey("alex", first); !errors.Is(err, ErrKeyExists) {
		t.Errorf("the same key twice: %v", err)
	}
	// The same key with another comment is still the same key.
	if _, err := users.AddSSHKey("alex", strings.Replace(first, "alex@laptop", "other", 1)); !errors.Is(err, ErrKeyExists) {
		t.Errorf("the same key with another comment: %v", err)
	}
	users.AddSSHKey("alex", second)
	user, err = users.RemoveSSHKey("alex", keys[0].ID)
	if err != nil || len(user.SSHKeys) != 1 || user.SSHKeys[0] != second {
		t.Errorf("after removing: %v %v", user.SSHKeys, err)
	}
	if _, err := users.RemoveSSHKey("alex", keys[0].ID); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("removing it again: %v", err)
	}
}

func TestOddSSHKeysAreRefused(t *testing.T) {
	users := newTestUsers(t)
	users.CreateOwner("alex", testPassword)
	key := testKey(t, "x")
	for name, line := range map[string]string{
		"empty":                     "",
		"not a key":                 "hello world",
		"a private key":             "-----BEGIN OPENSSH PRIVATE KEY-----",
		"with options":              `command="/bin/sh" ` + key,
		"with a restriction lifted": "no-pty,permitopen=\"*:*\" " + key,
		"two lines":                 key + "\n" + testKey(t, "y"),
		"broken base64":             "ssh-ed25519 AAAA!!!! x",
		"dsa":                       "ssh-dss AAAAB3NzaC1kc3MAAACBAJ x",
	} {
		if _, err := users.AddSSHKey("alex", line); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A comment cannot smuggle a second line into authorized_keys.
	user, err := users.AddSSHKey("alex", key+"\tevil\x00comment\x1b[31m")
	if err != nil || strings.ContainsAny(user.SSHKeys[0], "\x00\x1b\n") {
		t.Errorf("comment: %q %v", user.SSHKeys, err)
	}
	for range maxSSHKeys - 1 {
		if _, err := users.AddSSHKey("alex", testKey(t, "n")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := users.AddSSHKey("alex", testKey(t, "one too many")); !errors.Is(err, ErrTooManyKeys) {
		t.Errorf("more keys than allowed: %v", err)
	}
}
