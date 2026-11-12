// PLAN 79-7: the shapes of the NVR's API (docs/NVR.md). Times of phase 2
// (events, recordings, exports) are Unix seconds; the camera state and the
// failover carry ISO times.

export type DetectorKind = 'auto' | 'npu' | 'igpu' | 'coral' | 'hailo' | 'cpu';

export type NvrDevice = {
	kind: string;
	title: string;
	present: boolean;
	available: boolean;
	/** Why it cannot be used: driver, vm, plugin. */
	reason?: string;
};

export type NvrHardware = {
	devices: NvrDevice[];
	auto: string;
	videoDecode: boolean;
	accelDevice: boolean;
	avx2: boolean;
	memoryBytes: number;
	enrichmentsPossible: boolean;
	enrichmentsReason?: string;
};

export type NvrFailover = { from: string; to: string; at: string; reason: string; detail: string };
export type NvrLastError = { code: string; detail: string; at: string };

export type NvrDetector = {
	kind: DetectorKind;
	active: string;
	failover: NvrFailover | null;
	inferenceMs: number | null;
};

export type NvrStorage = {
	usedBytes: number | null;
	limitGB: number;
	recordingsBytes?: number | null;
	exportsBytes?: number | null;
	spanDays?: number | null;
	cap?: 'off' | 'ok' | 'cleaning' | 'exports_exceed';
	lastCleanup?: number | null;
};

export type NvrState =
	| 'off'
	| 'docker_off'
	| 'docker_starting'
	| 'no_volume'
	| 'pulling'
	| 'starting'
	| 'running'
	| 'stopped'
	| 'error'
	| 'unavailable';

/** GET /nvr/status; accounts without the admin role get only the first four fields. */
export type NvrStatus = {
	enabled: boolean;
	state: NvrState;
	cameras: { total: number; enabled: number; online: number };
	privacyNoticeDismissed: boolean;
	error?: NvrLastError | null;
	image?: {
		state: 'missing' | 'downloading' | 'ready' | 'error';
		version: string;
		bytes: number;
		size: number;
		progress: number;
		error?: string;
	};
	volume?: string;
	volumeMounted?: boolean;
	detector?: NvrDetector;
	storage?: NvrStorage;
	rtspOut?: boolean;
	frigateVersion?: string;
	hardware?: NvrHardware;
};

export type PlateEntry = { name: string; plates: string[] };

export type NvrSettings = {
	enabled: boolean;
	volume: string;
	detector: { kind: DetectorKind };
	storage: { limitGB: number };
	privacyNoticeDismissed: boolean;
	rtspOut: boolean;
	rtspUser: string;
	rtspPassword: string;
	plates?: PlateEntry[];
};

export type RecordMode = 'off' | 'events' | 'motion' | 'continuous';

export type CameraState = {
	online: boolean;
	known: boolean;
	fps: number;
	detectionFps: number;
	offlineSince: string | null;
};

export type CameraSource = {
	kind: 'rtsp' | 'onvif';
	host: string;
	port: number;
	onvifPort: number;
	user: string;
	hasPassword?: boolean;
	password?: string;
	clearPassword?: boolean;
	mainURL: string;
	subURL: string;
	allowPublic: boolean;
};

export type CameraDetect = { enabled: boolean; objects: string[]; plates: boolean; faces: boolean };
export type CameraRecord = {
	mode: RecordMode;
	retainDays: number;
	preSeconds: number;
	postSeconds: number;
};
export type CameraNotify = {
	titan: boolean;
	push: boolean;
	email: boolean;
	homeAssistant: boolean;
	agent: boolean;
	kinds: string[];
};

/** A camera as administrators get it; other accounts get the first block only. */
export type Camera = {
	id: string;
	name: string;
	enabled: boolean;
	order: number;
	audio: { enabled: boolean; twoWay: boolean };
	ptz: boolean;
	state: CameraState;
	rights?: { live: boolean; recordings: boolean };
	source?: CameraSource;
	detect?: CameraDetect;
	record?: CameraRecord;
	notify?: CameraNotify;
};

export type CameraList = { cameras: Camera[]; objects?: string[] };

export type Discovered = {
	address: string;
	onvifPort: number;
	name: string;
	hardware: string;
	id: string;
	known: boolean;
};

export type ProbeStream = {
	role: 'main' | 'sub';
	url: string;
	outcome: string;
	codec: string;
	audio: string;
	width: number;
	height: number;
};

export type ProbeResult = {
	outcome: string;
	device: { manufacturer: string; model: string; firmware: string } | null;
	profiles: { token: string; name: string; width: number; height: number; codec: string }[];
	mainURL: string;
	subURL: string;
	streams: ProbeStream[];
	ptz: boolean;
	onvifPort: number;
	clockOffsetSeconds: number;
};

export type Diagnosis = {
	state: NvrState;
	container: {
		state: string;
		exitCode: number;
		startedAt: string | null;
		finishedAt: string | null;
		recentEnds: number;
	};
	imageReference: string;
	imageVersion: string;
	imagePresent: boolean;
	runningVersion: string;
	detector: NvrDetector;
	detectorPid: number | null;
	lastError: NvrLastError | null;
	log: string[];
	logSource: string;
	accelDevice: boolean;
	npuFirmware: string[];
	gpuUsage: Record<string, unknown>;
	npuUsage: Record<string, unknown>;
	cameras: {
		id: string;
		name: string;
		online: boolean;
		known: boolean;
		fps: number;
		detectionFps: number;
		offlineSince: string | null;
		processFps: number;
		skippedFps: number;
	}[];
	shm: { totalMB: number | null; usedMB: number | null; freeMB: number | null };
	hardware: NvrHardware;
};

// ---- Phase 2 ----

export type NvrEvent = {
	id: string;
	camera: string;
	label: string;
	subLabel: string;
	plate: string;
	score: number;
	start: number;
	end: number | null;
	hasClip: boolean;
	hasSnapshot: boolean;
	zones: string[];
	reviewed: boolean;
	kept: boolean;
};
export type EventList = { events: NvrEvent[]; next: number | null };

export type FeedMessage =
	| { t: 'hello'; running: boolean }
	| { t: 'event'; phase: 'new' | 'update' | 'end'; event: NvrEvent }
	| { t: 'activity'; camera: string; motion: boolean; objects: string[] }
	| { t: 'bye'; reason: string }
	| { t: 'error'; code: string };

export type Segment = { start: number; end: number; motion: number; objects: number };
export type RecordingList = { from: number; to: number; segments: Segment[] };
export type RecordingSummary = {
	days: {
		day: string;
		hours: {
			hour: number;
			start: number;
			seconds: number;
			motion: number;
			objects: number;
			events: number;
		}[];
	}[];
};

export type NvrExport = {
	id: string;
	camera: string;
	name: string;
	from: number;
	to: number;
	created: number;
	state: 'running' | 'done' | 'failed';
	sizeBytes: number | null;
	owner: string;
};

export type PtzInfo = { supported: boolean; features: string[]; presets: string[] };

export type FaceAttempt = { file: string; at: number; guess: string; score: number };
export type Faces = {
	enabled: boolean;
	faces: { name: string; images: string[] }[];
	attempts: FaceAttempt[];
};
/** count 0, lastSeen null and camera "": read before the newest 1000 vehicle events. */
export type RecognizedPlate = {
	plate: string;
	count: number;
	lastSeen: number | null;
	camera: string;
};

export type UserCameras = {
	admin: boolean;
	cameras: { id: string; name: string; live: boolean; recordings: boolean }[];
};
