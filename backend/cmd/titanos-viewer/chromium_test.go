package main

import (
	"slices"
	"strings"
	"testing"
)

// PLAN 74: only one well-formed key reaches Chromium.
func TestTrustKeyGoesToChromiumOnlyWhenWellFormed(t *testing.T) {
	good := "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="
	for _, bad := range []string{"", "x", good + "," + good, "--no-sandbox", strings.Repeat("A", 44)} {
		if trustKey(bad) != "" {
			t.Errorf("%q accepted", bad)
		}
	}
	if trustKey(good) != good {
		t.Fatal("a good key was dropped")
	}
	args := chromeArgs(config{URL: "https://app:3001/", TrustKey: good, GPU: "cpu"}, 800, 600, 1)
	if !slices.Contains(args, "--ignore-certificate-errors-spki-list="+good) || args[len(args)-1] != "https://app:3001/" {
		t.Errorf("args: %v", args)
	}
	for _, a := range chromeArgs(config{URL: "https://app/"}, 800, 600, 1) {
		if strings.HasPrefix(a, "--ignore-certificate") {
			t.Errorf("without a key: %s", a)
		}
	}
}
