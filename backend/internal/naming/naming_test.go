package naming

import "testing"

func TestNamesKeepCaseAndRejectUnsafeSyntax(t *testing.T) {
	for _, name := range []string{"Backup", "Daten", "NAS-2", "backup"} {
		if !ValidVolume(name) || !ValidBackup(name) {
			t.Errorf("valid name rejected: %s", name)
		}
	}
	for _, name := range []string{"", "../Backup", "/Backup", " Backup", "Backup ", "-Backup", "A\nB"} {
		if ValidVolume(name) || ValidBackup(name) {
			t.Errorf("unsafe name accepted: %q", name)
		}
	}
	if ValidVolume("ABCDEFGHIJKLM") || ValidBackup("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef") {
		t.Fatal("length limit ignored")
	}
}
