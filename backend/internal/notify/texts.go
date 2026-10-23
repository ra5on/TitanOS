package notify

import "strings"

// What is sent out, per language. {s} stands for what the event is about:
// a volume or a disk. The web UI has its own texts for the same kinds.
var texts = map[string]map[string][2]string{
	"de": {
		"personal_digest":       {"{s} Meldungen während der Ruhezeit", ""},
		"personal_confirmation": {"Freigabe nötig: {s}", ""},
		"personal_done":         {"Erledigt: {s}", ""},
		"personal_blocked":      {"Kommt nicht weiter: {s}", ""},
		"assistant_done":        {"Agentenantwort abgeschlossen", "Der Auftrag von {s} hat eine Antwort. Ergebnisse stehen im KI-Assistenten."},
		"assistant_blocked":     {"Agent benötigt Aufmerksamkeit", "Ein Auftrag von {s} kann nicht weiterarbeiten. Einzelheiten stehen im KI-Assistenten."},
		"test":                  {"Testnachricht", "Wenn du das liest, funktioniert dieser Weg für Benachrichtigungen."},
		"volume_degraded":       {"Volume {s}: eine Platte fehlt", "Das Volume {s} läuft ohne Redundanz weiter. Ersetze die ausgefallene Platte bald; fällt eine weitere aus, sind die Daten verloren."},
		"volume_failed":         {"Volume {s} ist ausgefallen", "Dem Volume {s} fehlen zu viele Platten. Es ist nicht mehr benutzbar."},
		"volume_rebuilding":     {"Volume {s}: Wiederaufbau läuft", "Das Volume {s} wird wieder aufgebaut. Solange ist es noch nicht wieder voll geschützt."},
		"volume_ok":             {"Volume {s} ist wieder in Ordnung", "Das Volume {s} ist vollständig und synchron."},
		"volume_missing":        {"Volume {s} wurde nicht gefunden", "Die Platten des Volumes {s} sind nicht da. Freigaben, Apps und VMs darauf stehen still."},
		"volume_mismatch":       {"Volume {s}: Prüfung fand Abweichungen", "Die Prüfung des Volumes {s} hat Blöcke gefunden, deren Kopien sich unterscheiden. Sieh dir das Volume unter Speicher an."},
		"volume_full":           {"Volume {s} ist fast voll", "Auf dem Volume {s} sind mehr als 90 % belegt."},
		"disk_unhealthy":        {"Platte {s} meldet Fehler", "Die Selbstdiagnose (SMART) der Platte {s} meldet einen Fehler. Sichere die Daten und ersetze die Platte."},
		"disk_test_passed":      {"Platte {s} hat die Prüfung bestanden", "Die Prüfung der Platte {s} ist ohne Fehler durchgelaufen. Sie kann in ein Volume."},
		"disk_test_failed":      {"Platte {s} ist durch die Prüfung gefallen", "Die Prüfung der Platte {s} hat Fehler gefunden. Nimm sie nicht in ein Volume; Einzelheiten stehen unter Werkzeuge."},
		"ups_on_battery":        {"Stromausfall: das NAS läuft auf Batterie", "Die USV {s} versorgt das NAS. Kommt der Strom nicht zurück, fährt das NAS rechtzeitig herunter."},
		"ups_online":            {"Der Strom ist wieder da", "Die USV {s} hängt wieder am Netz."},
		"ups_low_battery":       {"Batterie der USV fast leer", "Die USV {s} meldet eine fast leere Batterie. Das NAS fährt jetzt herunter."},
		"ups_shutdown":          {"Das NAS fährt herunter", "Das NAS lief {s} Minuten auf Batterie und fährt jetzt herunter, wie eingestellt."},
		"ups_lost":              {"Keine Verbindung zur USV", "Die USV antwortet nicht mehr. Bei einem Stromausfall kann das NAS so nicht rechtzeitig herunterfahren."},
		"backup_failed":         {"Backup {s} fehlgeschlagen", "Das Backup {s} ist nicht durchgelaufen. Einzelheiten stehen unter Einstellungen › Backups."},
		"backup_overdue":        {"Backup {s} gelingt seit Längerem nicht", "Das Backup {s} hat mindestens zwei Termine und zwei Tage lang kein Backup zustande gebracht. Sieh unter Einstellungen › Backups nach, ob das Ziel erreichbar ist."},
		"update_available":      {"TitanOS {s} ist verfügbar", "Eine neue Version von TitanOS ist da. Du findest sie unter Einstellungen › Updates."},
		"update_installed":      {"TitanOS {s} läuft", "Das Update auf TitanOS {s} ist installiert und gestartet. Die vorherige Version bleibt als Rückfall auf dem Stick."},
		"update_failed":         {"Update auf TitanOS {s} fehlgeschlagen", "TitanOS {s} ist nicht gestartet. Das NAS läuft wieder mit der vorherigen Version."},
		"update_rolled_back":    {"Zurück auf TitanOS {s}", "Das NAS läuft wieder mit TitanOS {s}."},
		"certificate_expiring":  {"Das Zertifikat läuft bald ab", "Das eigene Zertifikat der Weboberfläche gilt nur noch bis {s}. Hinterlege unter Einstellungen › Zugang ein neues; sonst warnt der Browser."},
		"config_backup_failed":  {"Sicherung des Sticks fehlgeschlagen", "Die Konfiguration konnte nicht auf das Volume {s} gesichert werden. Sieh unter Einstellungen › Sicherung des Sticks nach."},
		"usb_plugged":           {"USB-Laufwerk {s} angeschlossen", "Unter Dateien › USB kannst du es einbinden und Dateien kopieren."},
		"disk_hot":              {"Platte {s} ist sehr warm", "Die Platte {s} ist wärmer als 55 °C. Prüfe die Lüftung."},
	},
	"en": {
		"personal_digest":       {"{s} messages during quiet hours", ""},
		"personal_confirmation": {"Approval needed: {s}", ""},
		"personal_done":         {"Done: {s}", ""},
		"personal_blocked":      {"Cannot continue: {s}", ""},
		"assistant_done":        {"Agent answer complete", "The task from {s} has an answer. See the assistant for results."},
		"assistant_blocked":     {"Agent needs attention", "A task from {s} cannot continue. See the assistant for details."},
		"test":                  {"Test message", "If you can read this, this way of notifying works."},
		"volume_degraded":       {"Volume {s}: a disk is missing", "The volume {s} goes on without redundancy. Replace the failed disk soon; if another one fails, the data is lost."},
		"volume_failed":         {"Volume {s} has failed", "The volume {s} lacks too many disks. It cannot be used any more."},
		"volume_rebuilding":     {"Volume {s}: rebuild in progress", "The volume {s} is being rebuilt. Until that is done it is not fully protected again."},
		"volume_ok":             {"Volume {s} is in order again", "The volume {s} is complete and in sync."},
		"volume_missing":        {"Volume {s} was not found", "The disks of the volume {s} are not there. Shares, apps and VMs on it stand still."},
		"volume_mismatch":       {"Volume {s}: the check found differences", "The check of the volume {s} found blocks whose copies differ. Have a look at the volume under Storage."},
		"volume_full":           {"Volume {s} is almost full", "More than 90 % of the volume {s} is in use."},
		"disk_unhealthy":        {"Disk {s} reports errors", "The self-diagnosis (SMART) of the disk {s} reports an error. Back up the data and replace the disk."},
		"disk_test_passed":      {"Disk {s} passed the test", "The test of the disk {s} ran through without errors. It can go into a volume."},
		"disk_test_failed":      {"Disk {s} failed the test", "The test of the disk {s} found errors. Do not put it into a volume; details are under Tools."},
		"ups_on_battery":        {"Power failure: the NAS runs on battery", "The UPS {s} powers the NAS. If the power does not come back, the NAS shuts down in time."},
		"ups_online":            {"The power is back", "The UPS {s} is on mains power again."},
		"ups_low_battery":       {"Battery of the UPS almost empty", "The UPS {s} reports an almost empty battery. The NAS shuts down now."},
		"ups_shutdown":          {"The NAS is shutting down", "The NAS ran {s} minutes on battery and shuts down now, as set."},
		"ups_lost":              {"No connection to the UPS", "The UPS does not answer any more. In a power failure the NAS cannot shut down in time like this."},
		"backup_failed":         {"Backup {s} failed", "The backup {s} did not run through. Details are under Settings › Backups."},
		"backup_overdue":        {"Backup {s} has not worked for a while", "The backup {s} has not made a backup for at least two of its runs and two days. Check under Settings › Backups whether the target can be reached."},
		"update_available":      {"TitanOS {s} is available", "A new version of TitanOS is out. You find it under Settings › Updates."},
		"update_installed":      {"TitanOS {s} is running", "The update to TitanOS {s} is installed and started. The former version stays on the stick as a fallback."},
		"update_failed":         {"Update to TitanOS {s} failed", "TitanOS {s} did not start. The NAS runs the former version again."},
		"update_rolled_back":    {"Back on TitanOS {s}", "The NAS runs TitanOS {s} again."},
		"certificate_expiring":  {"The certificate is about to expire", "The own certificate of the web interface is only valid until {s}. Put a new one in place under Settings › Access; otherwise the browser will warn."},
		"config_backup_failed":  {"Backup of the stick failed", "The configuration could not be saved to the volume {s}. Have a look at Settings › Backup of the stick."},
		"usb_plugged":           {"USB drive {s} plugged in", "Mount it under Files › USB to copy files."},
		"disk_hot":              {"Disk {s} is very warm", "The disk {s} is warmer than 55 °C. Check the ventilation."},
	},
}

// Wording returns title and text of an event in a language.
func Wording(language, kind, subject string) (string, string) {
	return wording(language, kind, subject)
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
