package plugins

import (
	"errors"
	"os"
	"testing"
)

// The tests here serve plugins from plain http test servers.
func TestMain(m *testing.M) {
	insecureForTests = true
	os.Exit(m.Run())
}

func TestPluginLinksNeedHTTPS(t *testing.T) {
	insecureForTests = false
	defer func() { insecureForTests = true }()
	for _, raw := range []string{"http://example.com/foo.plugin.json", "ftp://example.com/foo.plugin.json", "https://example.com/foo.plugin.json#x"} {
		if _, err := validLink(raw); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted", raw)
		}
	}
	if _, err := validLink("https://example.com/foo.plugin.json"); err != nil {
		t.Fatal(err)
	}
}
