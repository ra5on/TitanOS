package api

import (
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
