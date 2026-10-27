// Thin wrapper around fetch for the titand API.

export class ApiError extends Error {
	readonly status: number;
	readonly code: string;
	readonly retryAfter: number;
	constructor(
		status: number,
		/** Machine-readable reason, e.g. "invalid_credentials". Translated via `error.<code>`. */
		code: string,
		message: string,
		retryAfter = 0,
	) {
		super(message);
		this.status = status;
		this.code = code;
		this.retryAfter = retryAfter;
	}
}

type Body = Record<string, unknown> | Blob | FormData | undefined;

async function request<T>(
	method: string,
	path: string,
	body?: Body,
	signal?: AbortSignal,
): Promise<T> {
	const init: RequestInit = { method, credentials: 'same-origin', signal };
	if (body instanceof Blob || body instanceof FormData) {
		init.body = body;
	} else if (body !== undefined) {
		init.body = JSON.stringify(body);
		init.headers = { 'Content-Type': 'application/json' };
	}

	let res: Response;
	try {
		res = await fetch('/api/v1' + path, init);
	} catch {
		throw new ApiError(0, 'unreachable', 'titand is not reachable');
	}

	if (!res.ok) {
		let code = 'internal';
		let message = `HTTP ${res.status}`;
		let retryAfter = 0;
		try {
			const data = await res.json();
			code = data.error.code ?? code;
			message = data.error.message ?? message;
			retryAfter = data.error.retryAfter ?? 0;
		} catch {
			// Not a JSON error body, e.g. an answer from a proxy.
		}
		throw new ApiError(res.status, code, message, retryAfter);
	}
	// Many actions answer without a body (201, 202, 204).
	const text = await res.text();
	if (!text) return undefined as T;
	if (!res.headers.get('content-type')?.toLowerCase().includes('application/json')) {
		throw new ApiError(res.status, 'invalid_response', 'Expected a JSON API response');
	}
	try {
		return JSON.parse(text) as T;
	} catch {
		throw new ApiError(res.status, 'invalid_response', 'Invalid JSON API response');
	}
}

export const api = {
	get: <T>(path: string, signal?: AbortSignal) => request<T>('GET', path, undefined, signal),
	post: <T>(path: string, body?: Body, signal?: AbortSignal) =>
		request<T>('POST', path, body, signal),
	put: <T>(path: string, body?: Body) => request<T>('PUT', path, body),
	delete: <T>(path: string, body?: Body) => request<T>('DELETE', path, body),
};

export type Role = 'owner' | 'admin' | 'user';

export type User = { name: string; role: Role; twoFactor: boolean };

export type Session = {
	configAvailable: boolean;
	setupRequired: boolean;
	user: User | null;
	hostname: string;
	version?: string;
};

export type Accelerator = {
	address: string;
	name: string;
	vendor: string;
	kind: string;
	driver: string;
	state: 'ready' | 'driver_missing' | 'vm' | 'suspended';
	usagePercent: number | null;
	temperatureC: number | null;
	powerWatts: number | null;
	memoryUsedBytes: number | null;
	memoryTotalBytes: number | null;
	frequencyMHz: number | null;
	pState: string;
	engines: { name: string; usagePercent: number }[];
	history: (number | null)[];
};

export type DashboardNetwork = {
	interface: string;
	ipv4: string;
	ipv4Addresses: string[];
	ipv6Addresses: string[];
	gateway: string;
	gateway6: string;
	speedMbps: number;
	mac: string;
	mtu: number;
	state: string;
	rxBytesPerSecond: number;
	txBytesPerSecond: number;
};

export type SystemInfo = {
	inventory?: { mainboard: string; bios: string };
	accelerators: Accelerator[];
	hostname: string;
	version: string;
	kernel: string;
	uptimeSeconds: number;
	cpu: {
		model: string;
		threads: number;
		usagePercent: number;
		threadUsage?: { id: string; usagePercent: number | null }[];
	};
	memory: {
		totalBytes: number;
		usedBytes: number;
		freeBytes: number;
		availableBytes: number;
		cacheBytes?: number;
		dirtyBytes?: number;
		writebackBytes?: number;
		reserveBytes?: number;
		lowAvailable?: boolean;
		pressure?: { someAvg10: number; fullAvg10: number };
	};
	temperatures: Temperature[];
	image: {
		slot: string;
		sizeBytes: number;
		overlayUsedBytes: number;
		overlayTotalBytes: number;
	} | null;
	stick: { label: string; present: boolean };
	network: DashboardNetwork | null;
	networks: DashboardNetwork[];
	/** Seconds a network change on trial has left to be confirmed; 0 if there is none. */
	networkPendingSeconds: number;
	/** Number of the newest message; 0 if there is none or the account may not see them. */
	notificationsLatest: number;
};

export type Preferences = {
	shares: Record<string, unknown> | null;
	files: Record<string, unknown> | null;
	docker: Record<string, unknown> | null;
	vnc: Record<string, unknown> | null;
	sound: Record<string, unknown> | null;
	appearance: Record<string, unknown> | null;
	dashboard: Record<string, unknown> | null;
	assistant: Record<string, unknown> | null;
	bell: Record<string, unknown> | null;
};

export type Temperature = {
	id: string;
	chip: string;
	label: string;
	device: string;
	celsius: number | null;
	criticalC: number | null;
};

export type DiskRole = 'stick' | 'member' | 'free' | 'used';
export type MemberState = 'active' | 'rebuilding' | 'faulty' | 'spare';
export type VolumeState =
	'ok' | 'degraded' | 'rebuilding' | 'syncing' | 'checking' | 'failed' | 'missing';
export type RaidLevel = 'raid0' | 'raid1' | 'raid5' | 'raid6' | 'raid10';
/** Profiles of a Btrfs volume (internal/storage btrfsLayouts). */
export type BtrfsProfile = 'single' | 'raid1' | 'raid1c3';
export type Filesystem = 'ext4' | 'xfs' | 'btrfs';

export type Disk = {
	identity?: string;
	unavailableReason?: 'stick' | 'volume' | 'mounted' | 'testing' | 'small' | 'vm';
	name: string;
	path: string;
	model: string;
	serial: string;
	sizeBytes: number;
	kind: 'hdd' | 'ssd' | 'nvme' | 'usb';
	role: DiskRole;
	volume?: string;
	memberState?: MemberState;
	contents?: string[];
	mounted?: boolean;
	/** A test of the disk is running; nothing else may use it meanwhile. */
	testing?: boolean;
	smart: {
		healthy: boolean;
		temperatureC: number;
		powerOnHours: number;
		errors: number;
		rotationRate: number;
		wearPercent: number;
	} | null;
};

export type Volume = {
	name: string;
	level: RaidLevel;
	filesystem: Filesystem;
	mountPoint: string;
	mounted: boolean;
	state: VolumeState;
	sizeBytes: number;
	usedBytes: number;
	raidDisks: number;
	members: { disk: string; state: MemberState }[];
	sync: { percent: number; speedBytesPerSecond: number; remainingSeconds: number } | null;
	mismatches: number;
	/** Limits for shares: ext4/XFS only, switched on take effect at the next start. */
	shareQuotas: 'unsupported' | 'off' | 'pending' | 'active';
	shareQuotasError?: string;
	/** SSD cache of an mdadm volume (internal/storage cache.go). */
	cache?: VolumeCache;
};

export type CacheMode = 'writethrough' | 'writeback';
export type VolumeCache = {
	mode: CacheMode;
	disk: string;
	active: boolean;
	missing: boolean;
	removing: boolean;
	usedBlocks: number;
	totalBlocks: number;
	dirtyBlocks: number;
	readHitRate: number;
	blockBytes: number;
};

export type DiskSleepStatus = {
	idleMinutes: number;
	available: boolean;
	errors: { disk: string; message: string }[];
};
export type StorageStatus = { disks: Disk[]; volumes: Volume[]; diskSleep: DiskSleepStatus };

export type Account = {
	name: string;
	role: Role;
	created: string;
	smbReady: boolean;
	twoFactor: boolean;
	/** Locked: no login anywhere; files, shares and rights stay. */
	disabled: boolean;
	disabledAt?: string;
	disabledBy?: string;
};

export type AccessLevel = 'ro' | 'rw';

export type Share = {
	name: string;
	volume: string;
	smb: boolean;
	nfs: { enabled: boolean; clients: string[] | null; mode?: 'off' | 'ro' | 'rw' };
	access: Record<string, AccessLevel>;
	guest: boolean;
	browseable: boolean;
	recycle: { enabled: boolean; days: number };
	timeMachine: { enabled: boolean; sizeGB: number };
	/** Most the share may hold in bytes; absent for no limit. */
	quota?: number;
	quotaID?: number;
	quotaStatus?: ShareQuotaStatus;
	/** Btrfs snapshots as "Previous Versions" in Windows. */
	previousVersions?: boolean;
	/** Ransomware guard over SMB; guardLocked is set by the NAS only. */
	guard?: boolean;
	guardLocked?: { at: string; user: string; reason: string; files: number };
	path: string;
	available: boolean;
};

export type ShareQuotaStatus = {
	state: 'pending' | 'applying' | 'active' | 'error';
	used: number;
	limit: number;
	error?: string;
};

export type FileRoot = {
	id: string;
	name: string;
	kind: 'share' | 'volume' | 'usb' | 'stick';
	writable: boolean;
	recycle: boolean;
	recycleDays?: number;
	/** Text files can be saved although the root is read-only (the stick). */
	editable?: boolean;
	totalBytes: number;
	freeBytes: number;
	/** The full-text index covers (part of) this root. */
	indexed?: boolean;
};

export type CopyJob = {
	durable?: boolean;
	blocked?: boolean;
	waiting?: boolean;
	paused?: boolean;
	priority?: number;
	rate?: number;
	operation?: string;
	retryable?: boolean;
	id: string;
	move: boolean;
	state: 'running' | 'done' | 'failed' | 'cancelled';
	from: string;
	to: string;
	dir: string;
	names: string[];
	files: number;
	done: number;
	bytes: number;
	copied: number;
	current: string;
	error: string;
};

export type UsbDevice = {
	transport?: string;
	busy?: boolean;
	readOnly?: boolean;
	id: string;
	name: string;
	label: string;
	model: string;
	fsType: string;
	size: number;
	supported: boolean;
	mounted: boolean;
	mount: string;
	freeBytes: number;
	root: string;
};

export type FileEntry = { name: string; dir: boolean; size: number; modified: string };

export type UploadStatus = {
	id: string;
	offset: number;
	size: number;
	name: string;
	busy?: boolean;
};

export type DockerContainer = {
	id: string;
	name: string;
	image: string;
	state: 'running' | 'exited' | 'created' | 'restarting' | 'paused' | 'dead';
	since: string;
	ports: { host: number; container: number; protocol: string }[];
	app: string;
	service: string;
	managed: boolean;
	cpuPercent: number;
	memoryBytes: number;
	cpuHistory: number[];
	/** Value of the label titanos.webui: a host port, a port with a path or a URL. */
	webui: string;
	publicUrl: string;
	/** The app's own HTTPS name through TitanOS, e.g. https://immich.nas.local; missing without one. */
	proxyUrl?: string;
	icon: string;
	updateAvailable: boolean;
	updateState?: 'not_checked' | 'pinned' | 'failed' | 'current' | 'available';
	updateError?: string;
};

/** The HTTPS name of one app (GET /docker/proxy). */
export type AppProxyRoute = {
	app: string;
	enabled: boolean;
	name: string;
	port: number;
	scheme: 'http' | 'https';
	localOnly?: boolean;
	urls: string[];
};

export type AppProxyStatus = { domain?: string; suffixes: string[]; apps: AppProxyRoute[] };

export type DockerStatus = {
	settings: { enabled: boolean; volume: string };
	running: boolean;
	version: string;
	containers: DockerContainer[];
	apps: string[];
	jobs: Record<string, { state: 'running' | 'done' | 'failed'; output: string }>;
	updatesChecked: string;
};

export type AppSource = {
	id: string;
	name: string;
	url: string;
	builtin: boolean;
	format: 'titanos' | 'casaos' | 'compose' | '';
	count: number;
	updated: string;
	/** Why the last fetch failed: unreachable, empty. */
	error: string;
};

export type CatalogApp = {
	icon: string;
	id: string;
	source: string;
	name: string;
	category: string;
	/** Language to text; a template with a single text uses the key "". */
	description: Record<string, string>;
	image: string;
	services: number;
};

export type AppStore = { sources: AppSource[]; apps: CatalogApp[] };

export type VMDisk = {
	file: string;
	format: 'qcow2' | 'raw';
	sizeBytes: number;
	volume: string;
	/** Set for an existing image used as it is, or a whole disk (/dev/disk/by-id/…). */
	path?: string;
	/** The image this disk is being copied from. */
	source?: string;
};

export type VMFolder = { path: string; tag: string; readOnly: boolean };

export type VM = {
	directory?: string;
	name: string;
	displayName?: string;
	description: string;
	template: 'linux' | 'windows' | 'other';
	memoryMiB: number;
	vcpus: number;
	volume: string;
	disks: VMDisk[];
	iso: string;
	virtioISO?: string;
	autostart: boolean;
	/** Logical CPUs the vCPUs are tied to; empty lets Linux decide. */
	cpuPin: number[] | null;
	exclusive: boolean;
	folders: VMFolder[] | null;
	tpm: boolean;
	secureBoot: boolean;
	firmware?: 'uefi' | 'bios';
	pci: string[] | null;
	autoGPU?: boolean;
	usb: string[] | null;
	/** "bridge" for an address of its own in the network, otherwise "user". */
	network: 'user' | 'bridge' | `network:${string}`;
	mac: string;
	/** Set while disks are copied, or if that failed. */
	job: { operation?: string; state: 'running' | 'failed'; text: string } | null;
	state: 'running' | 'stopped';
	/** False while a volume of the VM is not mounted. */
	available: boolean;
	/** Load in percent of the VM's own vCPUs. */
	cpuPercent: number;
	/** Paused by QEMU after a disk error on the NAS; "resume" goes on. */
	ioError?: boolean;
	/** RAM of the NAS the VM occupies right now. */
	memoryBytes: number;
	since: string;
	/** Start and shut down at set times; missing means none. */
	powerSchedule?: import('./vm-schedule.ts').VMPowerSchedule;
	/** Next scheduled start/stop and what waits, while a schedule is on. */
	schedule?: import('./vm-schedule.ts').VMScheduleView;
};

export type VMStatus = {
 networks?: import('./network.ts').NASNetwork[];
	/** Whether the machine has hardware virtualization. */
	kvm: boolean;
	/** Whether the bridge is set up that gives VMs an address of their own. */
	bridge: boolean;
	host: {
		cpus: number;
		memoryBytes: number;
		usedMemoryBytes: number;
		availableMemoryBytes?: number;
		cpuModel: string;
	};
	vms: VM[];
};

export type HostCPU = { id: number; kind: '' | 'p' | 'e'; siblings: number[]; usedBy: string[] };
export type DiskImage = {
	path: string;
	format: string;
	virtualBytes: number;
	actualBytes: number;
	usedBy: string;
};
export type WholeDisk = { path: string; device: string; usedBy: string };
export type VMHost = { cpus: HostCPU[]; images: DiskImage[]; disks: WholeDisk[] };

export type PCIDevice = {
	address: string;
	vendor: string;
	device: string;
	vendorName: string;
	kind: 'igpu' | 'gpu' | 'npu';
	driver: string;
	iommuGroup: number;
	/** The display the firmware used: the console of the NAS. */
	primary: boolean;
	companions: string[];
	passable: boolean;
	/** Why it cannot be handed to a VM: no_iommu, shared_group, npu. */
	reason: string;
};
export type USBDevice = {
	id: string;
	name: string;
	port: string;
	stick: boolean;
	duplicate: boolean;
};
export type Hardware = {
	hardware: { iommu: boolean; pci: PCIDevice[]; usb: USBDevice[] };
	usedBy: Record<string, string[]>;
};
