package protect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The emergency handbook (PLAN 54-S): what the NAS is built of and what to
// do when something breaks, as Markdown (and PDF by the caller). Facts hold
// names, levels, models and serial numbers; never passwords or keys – the
// caller fills them only from such fields.

// DiskFact is a disk of a volume.
type DiskFact struct {
	Model  string `json:"model"`
	Serial string `json:"serial"`
	Size   uint64 `json:"size"`
	Kind   string `json:"kind,omitempty"`
	State  string `json:"state,omitempty"`
}

// VolumeFact is a volume with its disks.
type VolumeFact struct {
	Name       string     `json:"name"`
	Level      string     `json:"level"`
	Filesystem string     `json:"filesystem"`
	Size       uint64     `json:"size"`
	Encrypted  bool       `json:"encrypted,omitempty"`
	Disks      []DiskFact `json:"disks"`
}

// NamedFact is anything with a name and a short detail.
type NamedFact struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// Contact is someone to call (own entries of the owner).
type Contact struct {
	Name  string `json:"name"`
	Phone string `json:"phone,omitempty"`
	Note  string `json:"note,omitempty"`
}

// Facts are the inputs of the handbook.
type Facts struct {
	Host    string       `json:"host"`
	Version string       `json:"version,omitempty"`
	Volumes []VolumeFact `json:"volumes"`
	// Stick is the boot stick (model, size), when known.
	Stick   string      `json:"stick,omitempty"`
	Shares  []NamedFact `json:"shares"`
	Apps    []NamedFact `json:"apps"`
	VMs     []NamedFact `json:"vms"`
	Network []NamedFact `json:"network"`
	Remote  []NamedFact `json:"remote"`
	Backups []NamedFact `json:"backups"`
	// Probes are results of backup restore probes (optional).
	Probes []NamedFact `json:"probes,omitempty"`
	// Contacts are optional.
	Contacts []Contact `json:"contacts,omitempty"`
	// ConfigBackup says where the copy of the stick's settings is.
	ConfigBackup string `json:"configBackup,omitempty"`
}

// Hash identifies the facts: the handbook is made again only when it changes.
func (f Facts) Hash() string {
	b, _ := json.Marshal(f)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Section is one "Was tun, wenn …" of the handbook.
type Section struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Sub   string   `json:"sub"`
	Steps []string `json:"steps"`
}

func gb(b uint64) string {
	if b == 0 {
		return ""
	}
	if b >= 1e12 {
		return fmt.Sprintf("%.1f TB", float64(b)/1e12)
	}
	return fmt.Sprintf("%.0f GB", float64(b)/1e9)
}

// Level is a RAID level as people read it.
func Level(level string) string {
	switch strings.ToLower(level) {
	case "raid0":
		return "RAID 0"
	case "raid1":
		return "RAID 1"
	case "raid5":
		return "RAID 5"
	case "raid6":
		return "RAID 6"
	case "raid10":
		return "RAID 10"
	case "raid1c3":
		return "RAID1C3"
	case "single":
		return "single"
	}
	return level
}

func tolerates(level string) int {
	switch strings.ToLower(level) {
	case "raid1", "raid5", "raid10":
		return 1
	case "raid6", "raid1c3":
		return 2
	}
	return 0
}

// VolumeLine is the short line of a volume: level, file system, disks.
func VolumeLine(v VolumeFact, de bool) string {
	var disks []string
	for _, d := range v.Disks {
		s := strings.TrimSpace(d.Model)
		if d.Serial != "" {
			s += " (" + d.Serial + ")"
		}
		disks = append(disks, s)
	}
	parts := []string{Level(v.Level), v.Filesystem}
	if v.Encrypted {
		parts = append(parts, map[bool]string{true: "verschlüsselt", false: "encrypted"}[de])
	}
	line := strings.Join(parts, " · ")
	if len(disks) > 0 {
		line += " · " + strings.Join(disks, ", ")
	}
	return line
}

// Sections are the "Was tun, wenn …" parts for these facts.
func Sections(f Facts, de bool) []Section {
	t := func(d, e string) string {
		if de {
			return d
		}
		return e
	}
	var redundant, single []string
	for _, v := range f.Volumes {
		if tolerates(v.Level) > 0 {
			redundant = append(redundant, fmt.Sprintf("%s (%s, %d)", v.Name, Level(v.Level), tolerates(v.Level)))
		} else {
			single = append(single, v.Name)
		}
	}
	disk := Section{ID: "disk", Title: t("… eine Platte ausfällt", "… a disk fails"), Sub: t("welche Platte, welches Ersatzteil", "which disk, which replacement")}
	disk.Steps = []string{
		t("Ruhe bewahren und nichts formatieren. Eine Meldung von TitanOS nennt Volume und Seriennummer der Platte.", "Stay calm and format nothing. A message of TitanOS names the volume and the serial number of the disk."),
		t("Unter Speicher die Platte mit dieser Seriennummer suchen (Liste unten) und notieren, in welchem Schacht sie steckt (Knopf „Erkennen“ lässt sie blinken).", "Under Storage find the disk with that serial number (list below) and note its bay (the Identify button makes it blink)."),
		t("Ersatz: eine Platte gleicher oder größerer Kapazität, am besten gleiche Art (HDD/SSD).", "Replacement: a disk of the same or larger size, preferably the same kind (HDD/SSD)."),
	}
	if len(redundant) > 0 {
		disk.Steps = append(disk.Steps, t("Diese Volumes vertragen ausfallende Platten (Anzahl): ", "These volumes survive failed disks (count): ")+strings.Join(redundant, ", ")+". "+t("Die Daten bleiben lesbar, bis die Platte ersetzt ist.", "The data stays readable until the disk is replaced."))
	}
	if len(single) > 0 {
		disk.Steps = append(disk.Steps, t("Ohne Redundanz (eine Platte weg = Daten weg): ", "Without redundancy (one disk gone = data gone): ")+strings.Join(single, ", ")+". "+t("Hier hilft nur die Datensicherung.", "Only the backup helps here."))
	}
	disk.Steps = append(disk.Steps,
		t("NAS herunterfahren, Platte tauschen, starten; unter Speicher beim Volume „Platte ersetzen“ wählen. Der Wiederaufbau läuft im Hintergrund.", "Shut the NAS down, swap the disk, start it; under Storage choose “Replace disk” at the volume. The rebuild runs in the background."),
		t("Bis der Wiederaufbau fertig ist, keine zweite Platte ziehen.", "Do not pull a second disk before the rebuild is done."),
		t("RAID ersetzt keine Datensicherung: nach dem Tausch prüfen, ob die Sicherungen weiterlaufen.", "RAID is no backup: after the swap check that the backups still run."))

	boot := Section{ID: "boot", Title: t("… das NAS nicht startet", "… the NAS does not start"), Sub: t("Stick, Boot-Rückfall, Ersatzstick", "stick, boot fallback, spare stick")}
	boot.Steps = []string{
		t("Bildschirm anschließen. Steht dort eine Meldung, sie abfotografieren.", "Connect a screen. If a message shows, take a photo of it."),
		t("TitanOS startet vom USB-Stick mit dem Namen TITANOS. Nach einem missglückten Update startet es nach drei Versuchen von selbst die vorige Version.", "TitanOS starts from the USB stick labelled TITANOS. After a failed update it starts the previous version by itself after three tries."),
		t("Im Startmenü kann man den anderen Eintrag (A/B) von Hand wählen.", "In the boot menu the other entry (A/B) can be chosen by hand."),
		t("Startet gar nichts: Stick in einem anderen Rechner prüfen. Ist er kaputt, siehe „… der Stick kaputt ist“.", "If nothing starts: check the stick in another computer. If it is broken, see “… the stick is broken”."),
		t("Die Daten liegen auf den Platten, nicht auf dem Stick; sie bleiben erhalten.", "The data is on the disks, not on the stick; it stays."),
	}
	stick := Section{ID: "stick", Title: t("… der Stick kaputt ist", "… the stick is broken"), Sub: t("neuer Stick mit den Einstellungen", "new stick with the settings")}
	stick.Steps = []string{
		t("Auf github.com/ra5on/TitanOS das aktuelle Release laden und das ZIP auf einen FAT32-Stick mit dem Namen TITANOS entpacken.", "Download the current release from github.com/ra5on/TitanOS and unpack the ZIP onto a FAT32 stick labelled TITANOS."),
		t("Vom neuen Stick starten. Die Ersteinrichtung bietet an, die Einstellungen aus der Sicherung auf einem Volume zurückzuholen.", "Start from the new stick. The first setup offers to bring the settings back from the copy on a volume."),
	}
	if f.ConfigBackup != "" {
		stick.Steps = append(stick.Steps, t("Die Sicherung der Einstellungen liegt hier: ", "The copy of the settings is here: ")+f.ConfigBackup+".")
	} else {
		stick.Steps = append(stick.Steps, t("Achtung: Eine Sicherung der Einstellungen ist nicht eingerichtet (Einstellungen › Stick). Ohne sie müssen Konten und Freigaben neu angelegt werden; die Volumes werden unter Speicher übernommen.", "Note: no copy of the settings is set up (Settings › Stick). Without it, accounts and shares must be made again; volumes are taken over under Storage."))
	}
	data := Section{ID: "data", Title: t("… Daten zurückgeholt werden müssen", "… data must be brought back"), Sub: t("wo die Backups liegen, wie zurückstellen", "where the backups are, how to restore")}
	data.Steps = []string{
		t("Einzelne Dateien: in Windows „Vorherige Versionen“ oder unter Speicher › Snapshots (bei Btrfs).", "Single files: “Previous versions” in Windows, or Storage › Snapshots (Btrfs)."),
		t("Gelöschte Dateien liegen einige Tage im Papierkorb der Freigabe (Ordner .recycle).", "Deleted files stay in the share's recycle bin (.recycle) for some days."),
		t("Aus der Datensicherung: Einstellungen › Datensicherung › Zurückstellen.", "From the backup: Settings › Backups › Restore."),
	}
	if len(f.Backups) > 0 {
		var b []string
		for _, x := range f.Backups {
			s := x.Name
			if x.Detail != "" {
				s += " → " + x.Detail
			}
			b = append(b, s)
		}
		data.Steps = append(data.Steps, t("Die Sicherungen: ", "The backups: ")+strings.Join(b, "; ")+".")
	} else {
		data.Steps = append(data.Steps, t("Achtung: Es ist keine Datensicherung eingerichtet.", "Note: no backup is set up."))
	}
	return []Section{disk, boot, stick, data}
}

// Markdown is the handbook as a document.
func Markdown(f Facts, made time.Time, de bool) string {
	t := func(d, e string) string {
		if de {
			return d
		}
		return e
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s – %s\n\n", t("Notfallhandbuch", "Emergency handbook"), f.Host)
	fmt.Fprintf(&b, "%s %s. %s\n\n", t("Stand", "As of"), made.Format("02.01.2006 15:04"), t("Enthält keine Passwörter und keine Schlüssel. Ausdrucken und beim NAS aufbewahren.", "Holds no passwords and no keys. Print it and keep it with the NAS."))
	if f.Version != "" {
		fmt.Fprintf(&b, "TitanOS %s.\n\n", f.Version)
	}
	fmt.Fprintf(&b, "## %s\n\n", t("Aufbau", "Build"))
	fmt.Fprintf(&b, "### %s\n\n", t("Volumes und Platten", "Volumes and disks"))
	if len(f.Volumes) == 0 {
		b.WriteString(t("Keine Volumes.", "No volumes.") + "\n\n")
	}
	for _, v := range f.Volumes {
		fmt.Fprintf(&b, "**%s**: %s", v.Name, strings.TrimSpace(Level(v.Level)+" · "+v.Filesystem))
		if v.Size > 0 {
			fmt.Fprintf(&b, " · %s", gb(v.Size))
		}
		if v.Encrypted {
			b.WriteString(" · " + t("verschlüsselt (Passwort nicht hier)", "encrypted (password not here)"))
		}
		b.WriteString("\n\n")
		if len(v.Disks) > 0 {
			fmt.Fprintf(&b, "| %s | %s | %s |\n|---|---|---|\n", t("Modell", "Model"), t("Seriennummer", "Serial number"), t("Größe", "Size"))
			for _, d := range v.Disks {
				fmt.Fprintf(&b, "| %s | %s | %s |\n", cell(d.Model), cell(d.Serial), gb(d.Size))
			}
			b.WriteString("\n")
		}
	}
	if f.Stick != "" {
		fmt.Fprintf(&b, "%s: %s\n\n", t("Boot-Stick", "Boot stick"), f.Stick)
	}
	list := func(title string, items []NamedFact, empty string) {
		fmt.Fprintf(&b, "### %s\n\n", title)
		if len(items) == 0 {
			b.WriteString(empty + "\n\n")
			return
		}
		for _, it := range items {
			if it.Detail != "" {
				fmt.Fprintf(&b, "- **%s**: %s\n", it.Name, it.Detail)
			} else {
				fmt.Fprintf(&b, "- %s\n", it.Name)
			}
		}
		b.WriteString("\n")
	}
	list(t("Freigaben", "Shares"), f.Shares, t("Keine.", "None."))
	list(t("Wichtige Apps", "Important apps"), f.Apps, t("Keine.", "None."))
	list(t("Virtuelle Maschinen", "Virtual machines"), f.VMs, t("Keine.", "None."))
	list(t("Netzwerk", "Network"), f.Network, t("Unbekannt.", "Unknown."))
	list(t("Fernzugang", "Remote access"), f.Remote, t("Keiner eingerichtet.", "None set up."))
	list(t("Datensicherungen", "Backups"), f.Backups, t("Keine eingerichtet.", "None set up."))
	if len(f.Probes) > 0 {
		list(t("Letzte Wiederherstellungsproben", "Latest restore probes"), f.Probes, "")
	}
	fmt.Fprintf(&b, "## %s\n\n", t("Was tun, wenn …", "What to do when …"))
	for _, s := range Sections(f, de) {
		fmt.Fprintf(&b, "### %s\n\n", s.Title)
		for _, step := range s.Steps {
			fmt.Fprintf(&b, "1. %s\n", step)
		}
		b.WriteString("\n")
	}
	if len(f.Contacts) > 0 {
		fmt.Fprintf(&b, "## %s\n\n", t("Ansprechpartner", "Contacts"))
		for _, c := range f.Contacts {
			line := "- **" + c.Name + "**"
			if c.Phone != "" {
				line += ": " + c.Phone
			}
			if c.Note != "" {
				line += " – " + c.Note
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "/")
	if s == "" {
		return "–"
	}
	return s
}
