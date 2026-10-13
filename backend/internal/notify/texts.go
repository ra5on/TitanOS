package notify

import "strings"

// What is sent out, per language. {s} stands for what the event is about:
// a volume or a disk. The web UI has its own texts for the same kinds.
var texts = map[string]map[string][2]string{
	"de": {
		"test":                 {"Testnachricht", "Wenn du das liest, funktioniert dieser Weg für Benachrichtigungen."},
		"volume_degraded":      {"Volume {s}: eine Platte fehlt", "Das Volume {s} läuft ohne Redundanz weiter. Ersetze die ausgefallene Platte bald; fällt eine weitere aus, sind die Daten verloren."},
		"volume_failed":        {"Volume {s} ist ausgefallen", "Dem Volume {s} fehlen zu viele Platten. Es ist nicht mehr benutzbar."},
		"volume_rebuilding":    {"Volume {s}: Wiederaufbau läuft", "Das Volume {s} wird wieder aufgebaut. Solange ist es noch nicht wieder voll geschützt."},
		"volume_ok":            {"Volume {s} ist wieder in Ordnung", "Das Volume {s} ist vollständig und synchron."},
		"volume_missing":       {"Volume {s} wurde nicht gefunden", "Die Platten des Volumes {s} sind nicht da. Freigaben, Apps und VMs darauf stehen still."},
		"volume_mismatch":      {"Volume {s}: Prüfung fand Abweichungen", "Die Prüfung des Volumes {s} hat Blöcke gefunden, deren Kopien sich unterscheiden. Sieh dir das Volume unter Speicher an."},
		"volume_full":          {"Volume {s} ist fast voll", "Auf dem Volume {s} sind mehr als 90 % belegt."},
		"disk_unhealthy":       {"Platte {s} meldet Fehler", "Die Selbstdiagnose (SMART) der Platte {s} meldet einen Fehler. Sichere die Daten und ersetze die Platte."},
		"config_backup_failed": {"Sicherung des Sticks fehlgeschlagen", "Die Konfiguration konnte nicht auf das Volume {s} gesichert werden. Sieh unter Einstellungen › Sicherung des Sticks nach."},
		"disk_hot":             {"Platte {s} ist sehr warm", "Die Platte {s} ist wärmer als 55 °C. Prüfe die Lüftung."},
	},
	"en": {
		"test":                 {"Test message", "If you can read this, this way of notifying works."},
		"volume_degraded":      {"Volume {s}: a disk is missing", "The volume {s} goes on without redundancy. Replace the failed disk soon; if another one fails, the data is lost."},
		"volume_failed":        {"Volume {s} has failed", "The volume {s} lacks too many disks. It cannot be used any more."},
		"volume_rebuilding":    {"Volume {s}: rebuild in progress", "The volume {s} is being rebuilt. Until that is done it is not fully protected again."},
		"volume_ok":            {"Volume {s} is in order again", "The volume {s} is complete and in sync."},
		"volume_missing":       {"Volume {s} was not found", "The disks of the volume {s} are not there. Shares, apps and VMs on it stand still."},
		"volume_mismatch":      {"Volume {s}: the check found differences", "The check of the volume {s} found blocks whose copies differ. Have a look at the volume under Storage."},
		"volume_full":          {"Volume {s} is almost full", "More than 90 % of the volume {s} is in use."},
		"disk_unhealthy":       {"Disk {s} reports errors", "The self-diagnosis (SMART) of the disk {s} reports an error. Back up the data and replace the disk."},
		"config_backup_failed": {"Backup of the stick failed", "The configuration could not be saved to the volume {s}. Have a look at Settings › Backup of the stick."},
		"disk_hot":             {"Disk {s} is very warm", "The disk {s} is warmer than 55 °C. Check the ventilation."},
	},
}

// wording returns title and text of an event in a language.
func wording(language, kind, subject string) (string, string) {
	entry, known := texts[language][kind]
	if !known {
		if entry, known = texts["en"][kind]; !known {
			return kind, subject
		}
	}
	return strings.ReplaceAll(entry[0], "{s}", subject), strings.ReplaceAll(entry[1], "{s}", subject)
}
