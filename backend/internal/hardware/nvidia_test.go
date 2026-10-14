package hardware

import "testing"

func TestPowerLimitBounds(t *testing.T) {
	for _, test := range []struct {
		output  string
		percent int
		watts   float64
		ok      bool
	}{
		{"100, 250, 200", 0, 200, true}, {"100, 250, 200", 75, 150, true},
		{"100, 250, 200", 20, 0, false}, {"100, 250, 200", 101, 0, false},
		{"N/A, 250, 200", 80, 0, false}, {"100, 250, NaN", 80, 0, false},
		{"100, 250, +Inf", 80, 0, false}, {"100, 250, 200\n100, 250, 200", 80, 0, false},
	} {
		watts, err := powerWatts(test.output, test.percent)
		if (err == nil) != test.ok || test.ok && watts != test.watts {
			t.Fatalf("%+v: %v, %v", test, watts, err)
		}
	}
}
