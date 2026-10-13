// Thin wrapper around fetch for the titand API.

export class ApiError extends Error {
	constructor(
		readonly status: number,
		/** Machine-readable reason, e.g. "invalid_credentials". Translated via `error.<code>`. */
		readonly code: string,
		message: string,
		readonly retryAfter = 0
	) {
		super(message);
	}
}

type Body = Record<string, unknown> | Blob | undefined;

async function request<T>(method: string, path: string, body?: Body): Promise<T> {
	const init: RequestInit = { method, credentials: 'same-origin' };
	if (body instanceof Blob) {
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
	return (text ? JSON.parse(text) : undefined) as T;
}

export const api = {
	get: <T>(path: string) => request<T>('GET', path),
	post: <T>(path: string, body?: Body) => request<T>('POST', path, body),
	put: <T>(path: string, body?: Body) => request<T>('PUT', path, body),
	delete: <T>(path: string, body?: Body) => request<T>('DELETE', path, body)
};

export type Role = 'owner' | 'admin' | 'user';

export type User = { name: string; role: Role };

export type Session = {
	configAvailable: boolean;
	setupRequired: boolean;
	user: User | null;
	hostname: string;
	version?: string;
};

export type SystemInfo = {
	hostname: string;
	version: string;
	kernel: string;
	uptimeSeconds: number;
	cpu: { model: string; threads: number; usagePercent: number };
	memory: { totalBytes: number; usedBytes: number };
	image: { slot: string; sizeBytes: number; overlayUsedBytes: number; overlayTotalBytes: number } | null;
	stick: { label: string; present: boolean };
	network: {
		interface: string;
		ipv4: string;
		gateway: string;
		speedMbps: number;
		rxBytesPerSecond: number;
		txBytesPerSecond: number;
	} | null;
	/** Seconds a network change on trial has left to be confirmed; 0 if there is none. */
	networkPendingSeconds: number;
};

export type Preferences = {
	appearance: Record<string, unknown> | null;
	dashboard: Record<string, unknown> | null;
};

export type DiskRole = 'stick' | 'member' | 'free' | 'used';
export type MemberState = 'active' | 'rebuilding' | 'faulty' | 'spare';
export type VolumeState = 'ok' | 'degraded' | 'rebuilding' | 'syncing' | 'checking' | 'failed' | 'missing';
export type RaidLevel = 'raid0' | 'raid1' | 'raid5' | 'raid6' | 'raid10';
export type Filesystem = 'ext4' | 'xfs';

export type Disk = {
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
};

export type StorageStatus = { disks: Disk[]; volumes: Volume[] };

export type Account = { name: string; role: Role; created: string; smbReady: boolean };

export type AccessLevel = 'ro' | 'rw';

export type Share = {
	name: string;
	volume: string;
	smb: boolean;
	nfs: { enabled: boolean; clients: string[] | null };
	access: Record<string, AccessLevel>;
	guest: boolean;
	browseable: boolean;
	recycle: { enabled: boolean; days: number };
	timeMachine: { enabled: boolean; sizeGB: number };
	path: string;
	available: boolean;
};

export type FileRoot = {
	id: string;
	name: string;
	kind: 'share' | 'volume';
	writable: boolean;
	recycle: boolean;
	totalBytes: number;
	freeBytes: number;
};

export type FileEntry = { name: string; dir: boolean; size: number; modified: string };

export type UploadStatus = { id: string; offset: number; size: number; name: string };

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
	updateAvailable: boolean;
};

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
	name: string;
	description: string;
	template: 'linux' | 'windows' | 'other';
	memoryMiB: number;
	vcpus: number;
	volume: string;
	disks: VMDisk[];
	iso: string;
	autostart: boolean;
	/** Logical CPUs the vCPUs are tied to; empty lets Linux decide. */
	cpuPin: number[] | null;
	exclusive: boolean;
	folders: VMFolder[] | null;
	tpm: boolean;
	secureBoot: boolean;
	pci: string[] | null;
	usb: string[] | null;
	/** "bridge" for an address of its own in the network, otherwise "user". */
	network: 'user' | 'bridge';
	/** Set while disks are copied, or if that failed. */
	job: { state: 'running' | 'failed'; text: string } | null;
	state: 'running' | 'stopped';
	/** False while a volume of the VM is not mounted. */
	available: boolean;
	/** Load in percent of the VM's own vCPUs. */
	cpuPercent: number;
	/** RAM of the NAS the VM occupies right now. */
	memoryBytes: number;
	since: string;
};

export type VMStatus = {
	/** Whether the machine has hardware virtualization. */
	kvm: boolean;
	/** Whether the bridge is set up that gives VMs an address of their own. */
	bridge: boolean;
	host: { cpus: number; memoryBytes: number; usedMemoryBytes: number; cpuModel: string };
	vms: VM[];
};

export type HostCPU = { id: number; kind: '' | 'p' | 'e'; siblings: number[]; usedBy: string[] };
export type DiskImage = { path: string; format: string; virtualBytes: number; actualBytes: number; usedBy: string };
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
export type USBDevice = { id: string; name: string; port: string; stick: boolean; duplicate: boolean };
export type Hardware = { hardware: { iommu: boolean; pci: PCIDevice[]; usb: USBDevice[] }; usedBy: Record<string, string[]> };
