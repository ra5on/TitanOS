package webpush

import "testing"

// 1.39: the personal Agent's messages go to the Agent app on the home
// screen, like those of the earlier agent; others stay with TitanOS.
func TestAppOfPersonalAgent(t *testing.T) {
	for tag, want := range map[string]string{"personal-agent-j1": AppAgent, "agent-c1": AppAgent, "backup-1": AppTitanOS, "": AppTitanOS} {
		if got := AppOf(Message{Tag: tag}); got != want {
			t.Errorf("%q: %s, want %s", tag, got, want)
		}
	}
}
