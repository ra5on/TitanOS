package api

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// Review PLAN 85-4: an account another node of the cluster deleted leaves
// the cameras here too, as a deletion on this NAS does.
func TestClusterDeletionForgetsCameraRights(t *testing.T) {
	ts, manager := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	ts.guest("gast")
	grant(t, ts, "gast", map[string]string{"einfahrt": "both"})
	if !manager.Access.Allows("gast", "einfahrt", nvr.RightLive) {
		t.Fatal("the right was not given")
	}
	user, ok := ts.users.Get("gast")
	if !ok {
		t.Fatal("no account")
	}
	ts.server.clusterArea().clusterApplied(user, true, user, true)
	if manager.Access.Allows("gast", "einfahrt", nvr.RightLive) || manager.Access.Allows("gast", "einfahrt", nvr.RightRecordings) {
		t.Error("the camera rights of the deleted account stayed")
	}
}

// Review PLAN 85-4: a list in a request replaces the stored one as a
// whole. Decoded over the stored entries by position, an entry left
// partly out took the values that stood there before.
func TestNVRRequestListsReplaceAsAWhole(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	// The rules of the condition on Home Assistant.
	rule := func(rules ...map[string]any) map[string]any {
		return map[string]any{"notify": map[string]any{"ha": map[string]any{"mode": "any", "rules": rules}}}
	}
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", rule(map[string]any{"connection": "zweit", "entity": "alarm_control_panel.haus", "states": []string{"armed_away"}})); res.StatusCode != 200 {
		t.Fatalf("first rule: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", rule(map[string]any{"entity": "person.anna", "states": []string{"not_home"}})); res.StatusCode != 200 {
		t.Fatalf("second rule: %d %s", res.StatusCode, body)
	}
	var view struct {
		Notify struct {
			HA struct {
				Rules []struct {
					Connection string `json:"connection"`
					Entity     string `json:"entity"`
				} `json:"rules"`
			} `json:"ha"`
		} `json:"notify"`
	}
	getJSON(t, ts, ts.client, "/nvr/cameras/haustuer", &view)
	if rules := view.Notify.HA.Rules; len(rules) != 1 || rules[0].Connection != "" || rules[0].Entity != "person.anna" {
		t.Errorf("rules after the change: %+v", rules)
	}
	// Known plates: an entry without its name does not take the stored one.
	if res, body := ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"plates": []map[string]any{{"name": "Annas Auto", "plates": []string{"M-AB 1234"}}}}); res.StatusCode != 200 {
		t.Fatalf("plates: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"plates": []map[string]any{{"plates": []string{"K-XY 99"}}}}); res.StatusCode != 400 {
		t.Errorf("a plate without a name: %d, want 400", res.StatusCode)
	}
	var settings struct {
		Plates []struct {
			Name   string   `json:"name"`
			Plates []string `json:"plates"`
		} `json:"plates"`
	}
	getJSON(t, ts, ts.client, "/nvr/settings", &settings)
	if len(settings.Plates) != 1 || settings.Plates[0].Name != "Annas Auto" || settings.Plates[0].Plates[0] != "M-AB 1234" {
		t.Errorf("plates after the refused change: %+v", settings.Plates)
	}
}

// Owner's log 13.11.2026: "write response … broken pipe" from a client
// that went away stood as an error. It is not one.
func TestClientGoneIsNoError(t *testing.T) {
	gone := &net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.EPIPE)}
	if !clientGone(gone) || !clientGone(fmt.Errorf("wrapped: %w", gone)) || !clientGone(net.ErrClosed) {
		t.Error("a closed connection counts as an error")
	}
	if clientGone(errors.New("json: unsupported value: NaN")) {
		t.Error("an encoding error is not a gone client")
	}
}
