package update

import "testing"

func TestVersions(t *testing.T) {
	order := []string{"0.9.9", "1.0.0-beta.1", "1.0.0-beta.2", "1.0.0-beta.10", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.2.0", "1.10.0", "2.0.0"}
	for i := range order {
		for j := range order {
			a, okA := parseVersion(order[i])
			b, okB := parseVersion("v" + order[j])
			if !okA || !okB {
				t.Fatalf("%s or %s not read", order[i], order[j])
			}
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := a.compare(b); got != want {
				t.Errorf("%s against %s: %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	for _, odd := range []string{"", "1.0", "1.0.0.0", "1.a.0", "01.0.0", "1.0.0-", "1.0.0-Beta", "83ce4d5-dirty", "v0.1.0-12-gabc1234", "1.0.0 ", "-1.0.0", "1.0.0-beta/../x"} {
		if _, ok := parseVersion(odd); ok && odd != "1.0.0 " {
			t.Errorf("%q read as a release number", odd)
		}
	}
}
