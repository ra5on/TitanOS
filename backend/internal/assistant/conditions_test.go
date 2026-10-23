package assistant

import (
	"testing"
	"time"
)

func TestTimeWindowAcrossMidnightAndWeekdays(t *testing.T) {
	w := TimeWindow{Start: "22:00", End: "06:00", Timezone: "Europe/Berlin", Weekdays: []int{5}}
	for _, test := range []struct {
		stamp string
		want  bool
	}{{"2026-10-23T23:00:00+02:00", true}, {"2026-10-24T05:59:00+02:00", true}, {"2026-10-24T06:00:00+02:00", false}, {"2026-10-24T23:00:00+02:00", false}} {
		now, _ := time.Parse(time.RFC3339, test.stamp)
		if got := w.Contains(now); got != test.want {
			t.Errorf("%s: %v", test.stamp, got)
		}
	}
	if err := (Conditions{Window: &TimeWindow{Start: "bad", End: "06:00", Timezone: "Europe/Berlin"}}).Validate(); err == nil {
		t.Fatal("bad time accepted")
	}
	if err := (Conditions{States: []StateCondition{{Entity: "light.one", Operator: "eq", ForSeconds: -1}}}).Validate(); err == nil {
		t.Fatal("negative duration accepted")
	}
}

func TestAutomationHistoryBoundedAndUnforgeable(t *testing.T) {
	dir := t.TempDir()
	store := NewAutomations(func() (string, bool) { return dir, true })
	a := Automation{Name: "original", Prompt: "report", Enabled: true, Trigger: Trigger{Kind: "interval", Minutes: 60}}
	var err error
	a, err = store.Put("alex", a, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for range 8 {
		a.Name = "edited"
		a.Versions = []AutomationDefinition{{Name: "injected"}}
		a, err = store.Put("alex", a, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(a.Versions) != 5 {
		t.Fatal(len(a.Versions))
	}
	for _, v := range a.Versions {
		if v.Name == "injected" {
			t.Fatal("client forged history")
		}
	}
}
