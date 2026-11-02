// The hardware compatibility check (GET /hardware/report, /hardware/disks).
// The backend only reads sysfs, procfs and the kernel log.

export type PluginHint = { plugin: string; title: string; installed: boolean };

export type UnboundDevice = {
	bus: 'pci' | 'usb';
	address: string;
	id: string;
	class: string;
	name: string;
	hint: 'plugin' | 'kernel' | 'driver_failed' | 'not_needed' | 'unsupported';
	plugins?: PluginHint[];
	/** hint kernel: first kernel with a driver; driver_failed: first kernel with the fix. */
	kernel?: string;
	driver?: string;
	/** hint driver_failed: the driver's kernel lines about the device. */
	messages?: string[];
};

export type FirmwareMissing = { file: string; device?: string };

export type Hint = { code: string; setting?: string };

export type HwDisk = {
	name: string;
	model: string;
	sizeBytes: number;
	transport: 'sata' | 'sas' | 'nvme' | 'usb' | 'virtio' | 'mmc' | 'other';
	rotational: boolean;
	removable: boolean;
	zoned?: string;
	smr?: 'zoned' | 'known_model';
	controller?: string;
	usb?: string;
	usbDriver?: string;
	hints: Hint[];
};

export type Virtualization = {
	vendor: 'intel' | 'amd' | 'other';
	cpuFlag: boolean;
	kvm: boolean;
	disabledByFirmware: boolean;
	iommu: boolean;
	iommuTable: boolean;
};

export type HwCPU = {
	vendor: string;
	model: string;
	threads: number;
	cores: number;
	level: number;
	features: string[];
};

export type Finding = { level: 'warn' | 'info'; code: string; subject?: string };

export type HardwareReport = {
	generated: string;
	kernel: string;
	board: string;
	boot: 'uefi' | 'bios';
	cpu: HwCPU;
	virtualization: Virtualization;
	memory: { totalBytes: number; ecc: boolean; corrected: number; uncorrected: number };
	controllers: {
		address: string;
		id: string;
		name: string;
		kind: string;
		driver: string;
		hints: Hint[];
	}[];
	disks: HwDisk[];
	network: {
		name: string;
		driver: string;
		address: string;
		bus: string;
		speedMbs: number;
		link: boolean;
	}[];
	graphics: { address: string; id: string; name: string; driver: string }[];
	sensors: { chip: string; temps: number; fans: number }[];
	unbound: UnboundDevice[];
	firmwareMissing: FirmwareMissing[];
	summary: Finding[];
};

/** How an instruction set reads: AVX2, SSE4.2, x86-64-v3. */
export function needLabel(need: string): string {
	if (need.startsWith('x86-64-')) return need;
	if (need === 'pni') return 'SSE3';
	return need.replace('_', '.').toUpperCase();
}

export function needList(needs: string[]): string {
	return needs.map(needLabel).join(', ');
}

/** The selected disks that are SMR, when the level is RAID 5 or 6. */
export function smrForLevel(level: string, selected: string[], disks: HwDisk[]): HwDisk[] {
	if (level !== 'raid5' && level !== 'raid6') return [];
	const names = new Set(selected.map((name) => name.replace(/^\/dev\//, '')));
	return disks.filter((disk) => !!disk.smr && names.has(disk.name));
}

export type VirtAdvice = { level: 'ok' | 'warn' | 'info'; key: string };

/** What to say about VT-x/AMD-V and the IOMMU, as i18n keys. */
export function virtAdvice(v: Virtualization): VirtAdvice[] {
	const list: VirtAdvice[] = [];
	if (!v.cpuFlag || !v.kvm) {
		list.push({ level: 'warn', key: `hwcheck.virt.off.${v.vendor}` });
		if (v.disabledByFirmware) list.push({ level: 'warn', key: 'hwcheck.virt.byFirmware' });
		return list;
	}
	list.push({ level: 'ok', key: 'hwcheck.virt.ok' });
	if (v.iommu) list.push({ level: 'ok', key: 'hwcheck.iommu.ok' });
	else if (v.iommuTable && v.vendor !== 'other')
		list.push({ level: 'info', key: `hwcheck.iommu.table.${v.vendor}` });
	else list.push({ level: 'info', key: `hwcheck.iommu.off.${v.vendor}` });
	return list;
}

/** Devices that need attention (leaves out those that need no driver). */
export function attention(devices: UnboundDevice[]): UnboundDevice[] {
	return devices.filter((device) => device.hint !== 'not_needed');
}
