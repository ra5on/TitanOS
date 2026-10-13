package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// blockDevice is one entry of `lsblk --json`.
type blockDevice struct {
	Name       string        `json:"name"`  // /dev/sda
	KName      string        `json:"kname"` // /dev/sda
	Type       string        `json:"type"`  // disk, part, raid5, loop, rom …
	Size       uint64        `json:"size"`
	Rota       bool          `json:"rota"`
	Removable  bool          `json:"rm"`
	Transport  string        `json:"tran"`
	Model      string        `json:"model"`
	Serial     string        `json:"serial"`
	FSType     string        `json:"fstype"`
	Label      string        `json:"label"`
	UUID       string        `json:"uuid"`
	Mountpoint string        `json:"mountpoint"`
	Children   []blockDevice `json:"children"`
}

var lsblkArgs = []string{"--json", "--bytes", "--paths", "--output",
	"NAME,KNAME,TYPE,SIZE,ROTA,RM,TRAN,MODEL,SERIAL,FSTYPE,LABEL,UUID,MOUNTPOINT"}

func parseLsblk(output string) ([]blockDevice, error) {
	var result struct {
		Blockdevices []blockDevice `json:"blockdevices"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, err
	}
	return result.Blockdevices, nil
}

// walk calls fn for the device and everything below it.
func (d blockDevice) walk(fn func(blockDevice)) {
	fn(d)
	for _, child := range d.Children {
		child.walk(fn)
	}
}

// partitions returns the direct children of type "part".
func (d blockDevice) partitions() []blockDevice {
	var parts []blockDevice
	for _, child := range d.Children {
		if child.Type == "part" {
			parts = append(parts, child)
		}
	}
	return parts
}

// isArray reports whether an lsblk entry is an md device sitting on top of
// a disk rather than something stored on the disk itself.
func isArray(d blockDevice) bool {
	return strings.HasPrefix(d.Type, "raid") || d.Type == "md" || d.Type == "linear"
}

// classify turns the lsblk tree of one disk into what TitanOS shows.
// volumeByArray maps the UUID of an md array to the volume it belongs to.
func classify(d blockDevice, volumeByArray map[string]string) Disk {
	disk := Disk{
		Name:      strings.TrimPrefix(d.Name, "/dev/"),
		Path:      d.Name,
		Model:     strings.TrimSpace(d.Model),
		Serial:    strings.TrimSpace(d.Serial),
		SizeBytes: d.Size,
		Kind:      diskKind(d),
	}

	stick := false
	var inspect func(node blockDevice)
	inspect = func(node blockDevice) {
		if isArray(node) {
			// The filesystem of a volume lives on the array, not on this disk.
			return
		}
		if node.Mountpoint != "" {
			disk.Mounted = true
		}
		switch {
		case node.FSType == "vfat" && node.Label == stickLabel:
			stick = true
		case node.FSType == "linux_raid_member":
			if volume, ok := volumeByArray[normalizeUUID(node.UUID)]; ok {
				disk.Volume = volume
			} else {
				disk.Contents = appendUnique(disk.Contents, "RAID")
			}
		case node.FSType != "":
			disk.Contents = appendUnique(disk.Contents, node.FSType)
		}
		for _, child := range node.Children {
			inspect(child)
		}
	}
	inspect(d)

	switch {
	case stick:
		disk.Role, disk.Volume = RoleStick, ""
	case disk.Volume != "":
		disk.Role = RoleMember
	case len(disk.Contents) > 0 || len(d.partitions()) > 0:
		disk.Role = RoleUsed
	default:
		disk.Role = RoleFree
	}
	return disk
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func diskKind(d blockDevice) string {
	switch {
	case d.Transport == "nvme":
		return "nvme"
	case d.Transport == "usb":
		return "usb"
	case d.Rota:
		return "hdd"
	default:
		return "ssd"
	}
}

// isDisk reports whether an lsblk entry is a real disk (not a loop device,
// an optical drive or an array).
func isDisk(d blockDevice) bool {
	return d.Type == "disk" && d.Size > 0 && !strings.HasPrefix(d.Name, "/dev/zram")
}

// normalizeUUID makes the two spellings of an md array UUID comparable:
// lsblk prints 8-4-4-4-12 with dashes, mdadm 8:8:8:8 with colons.
func normalizeUUID(uuid string) string {
	return strings.ToLower(strings.NewReplacer("-", "", ":", "").Replace(uuid))
}

// array is the live state of one md device, read from /sys.
type array struct {
	device   string // md127
	uuid     string // normalized
	level    string // raid5
	state    string // clean, active, inactive, …
	degraded int
	disks    int
	sectors  uint64 // 512-byte sectors

	syncAction    string // idle, resync, recover, check, repair
	syncDone      uint64 // sectors
	syncTotal     uint64
	syncSpeedKiB  uint64
	mismatchCount uint64

	members []arrayMember
}

type arrayMember struct {
	device string // sda1
	state  string // in_sync, faulty, spare, …
	slot   string // "0", "1", … or "none"
}

func readSysfs(path string) string {
	data, _ := os.ReadFile(path)
	return strings.TrimSpace(string(data))
}

func readSysfsUint(path string) uint64 {
	value, _ := strconv.ParseUint(readSysfs(path), 10, 64)
	return value
}

// readArrays lists the md devices that exist right now. uuidOf returns the
// UUID of an array (asked from mdadm, the kernel does not tell).
func readArrays(sysBlock string, uuidOf func(device string) string) []array {
	entries, _ := filepath.Glob(filepath.Join(sysBlock, "md*", "md"))
	var arrays []array
	for _, md := range entries {
		device := filepath.Base(filepath.Dir(md))
		a := array{
			device:        device,
			level:         readSysfs(filepath.Join(md, "level")),
			state:         readSysfs(filepath.Join(md, "array_state")),
			degraded:      int(readSysfsUint(filepath.Join(md, "degraded"))),
			disks:         int(readSysfsUint(filepath.Join(md, "raid_disks"))),
			sectors:       readSysfsUint(filepath.Join(filepath.Dir(md), "size")),
			syncAction:    readSysfs(filepath.Join(md, "sync_action")),
			syncSpeedKiB:  readSysfsUint(filepath.Join(md, "sync_speed")),
			mismatchCount: readSysfsUint(filepath.Join(md, "mismatch_cnt")),
		}
		a.syncDone, a.syncTotal = parseSyncCompleted(readSysfs(filepath.Join(md, "sync_completed")))
		members, _ := filepath.Glob(filepath.Join(md, "dev-*"))
		for _, member := range members {
			a.members = append(a.members, arrayMember{
				device: strings.TrimPrefix(filepath.Base(member), "dev-"),
				state:  readSysfs(filepath.Join(member, "state")),
				slot:   readSysfs(filepath.Join(member, "slot")),
			})
		}
		a.uuid = normalizeUUID(uuidOf(device))
		arrays = append(arrays, a)
	}
	return arrays
}

// parseSyncCompleted parses "123456 / 7890123" (sectors); "none" is 0, 0.
func parseSyncCompleted(value string) (done, total uint64) {
	left, right, ok := strings.Cut(value, "/")
	if !ok {
		return 0, 0
	}
	done, _ = strconv.ParseUint(strings.TrimSpace(left), 10, 64)
	total, _ = strconv.ParseUint(strings.TrimSpace(right), 10, 64)
	return done, total
}

// parseMdadmExport reads KEY=value lines of `mdadm --detail --export`.
func parseMdadmExport(output string) map[string]string {
	values := map[string]string{}
	for line := range strings.SplitSeq(output, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			values[key] = value
		}
	}
	return values
}

// memberState translates the kernel's view of an array member.
func memberState(m arrayMember) string {
	switch {
	case strings.Contains(m.state, "faulty"):
		return MemberFaulty
	case strings.Contains(m.state, "in_sync"):
		return MemberActive
	case m.slot != "none" && m.slot != "":
		// Not in sync yet but already has its place: being rebuilt.
		return MemberRebuilding
	default:
		return MemberSpare
	}
}

// smartctlArgs asks for health, attributes and identity as JSON. "-n standby"
// leaves a sleeping disk asleep.
var smartctlArgs = []string{"--json=c", "--health", "--attributes", "--info", "--nocheck=standby"}

func parseSmart(output string) *Smart {
	var data struct {
		SmartSupport *struct {
			Available bool `json:"available"`
		} `json:"smart_support"`
		SmartStatus *struct {
			Passed bool `json:"passed"`
		} `json:"smart_status"`
		Temperature *struct {
			Current int `json:"current"`
		} `json:"temperature"`
		PowerOnTime *struct {
			Hours int `json:"hours"`
		} `json:"power_on_time"`
		RotationRate int `json:"rotation_rate"`
		ATA          *struct {
			Table []struct {
				ID  int `json:"id"`
				Raw struct {
					Value uint64 `json:"value"`
				} `json:"raw"`
			} `json:"table"`
		} `json:"ata_smart_attributes"`
		NVMe *struct {
			PercentageUsed int    `json:"percentage_used"`
			MediaErrors    uint64 `json:"media_errors"`
		} `json:"nvme_smart_health_information_log"`
	}
	if json.Unmarshal([]byte(output), &data) != nil || data.SmartStatus == nil {
		return nil // no SMART: virtual disk, USB bridge without support, …
	}
	smart := &Smart{Healthy: data.SmartStatus.Passed, RotationRate: data.RotationRate, WearPercent: -1}
	if data.Temperature != nil {
		smart.TemperatureC = data.Temperature.Current
	}
	if data.PowerOnTime != nil {
		smart.PowerOnHours = data.PowerOnTime.Hours
	}
	if data.ATA != nil {
		for _, attribute := range data.ATA.Table {
			switch attribute.ID {
			case 5, 197, 198: // reallocated, pending, uncorrectable sectors
				smart.Errors += attribute.Raw.Value
			}
		}
	}
	if data.NVMe != nil {
		smart.Errors += data.NVMe.MediaErrors
		smart.WearPercent = data.NVMe.PercentageUsed
	}
	return smart
}

// partitionPath returns the first partition of a disk: /dev/sda1, /dev/nvme0n1p1.
func partitionPath(disk string) string {
	if last := disk[len(disk)-1]; last >= '0' && last <= '9' {
		return disk + "p1"
	}
	return disk + "1"
}
