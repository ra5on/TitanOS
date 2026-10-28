package run

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunSecretPassesStdinAndDescriptors(t *testing.T) {
	out, err := Exec{}.RunSecret(context.Background(), []byte("old secret"), [][]byte{[]byte("new secret"), []byte("third")},
		"sh", "-c", `cat; printf '|'; cat /dev/fd/3; printf '|'; cat /dev/fd/4`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "old secret|new secret|third" {
		t.Fatalf("%q", out)
	}
}

func TestRunSecretErrorNeverCarriesTheSecret(t *testing.T) {
	_, err := Exec{}.RunSecret(context.Background(), []byte("hunter2-secret"), [][]byte{[]byte("other-secret")}, "sh", "-c", "exit 2")
	var command *CommandError
	if !errors.As(err, &command) {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("secret in error: %v", err)
	}
}
