package version

import "testing"

func TestDisplay(t *testing.T) {
	for in, want := range map[string]string{
		"0.72.0": "0.72", "0.100.0": "0.100", "0.71.1": "0.71.1", "0.70.10": "0.70.10",
		"1.0.0": "1.00", "1.3.0": "1.03", "1.12.0": "1.12", "2.0.0": "2.00", "1.0.1": "1.0.1",
		"0.72.0-rc.1": "0.72.0-rc.1", "dev": "dev",
	} {
		if got := Display(in); got != want {
			t.Errorf("Display(%q) = %q, want %q", in, got, want)
		}
	}
}
