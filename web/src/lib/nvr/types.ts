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

/** A GPU that can decode the camera streams (PLAN 82-4). */
export type NvrDecoder = {
	/** PCI address, what the setting `decoder` names. */
	id: string;
	title: string;
	kind: 'igpu' | 'gpu' | 'nvidia' | string;
	available: boolean;
	/** Why it cannot decode: driver, vm. */
	reason?: string;
};

/** The GPU that decodes, as the setting resolves now. */
export type NvrDecoderStatus = {
	/** 'auto', 'cpu' or a PCI address. */
	setting: string;
	/** PCI address of the GPU that decodes, or 'cpu'. */
	active: string;
	kind: string;
	title: string;
	/** Why a chosen GPU does not decode: gone, vm, driver. */
	reason?: string;
};

export type NvrHardware = {
	devices: NvrDevice[];
	auto: string;
	/** Absent from a NAS before PLAN 82. */
	decoders?: NvrDecoder[];
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
	decoder?: NvrDecoderStatus;
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
	/** Main switch of the cameras' messages (PLAN 82-3); absent is on. */
	notifyOff?: boolean;
	/** GPU that decodes the streams (PLAN 82-4): '' / 'auto', 'cpu' or a PCI address. */
	decoder?: string;
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
	/** Picture sizes from the last check (set by the NAS; 0 while unknown). */
	mainWidth?: number;
	mainHeight?: number;
	subWidth?: number;
	subHeight?: number;
};

/** Which stream serves what (PLAN 82-2); '' is the default of each. */
export type StreamChoice = '' | 'main' | 'sub';
export type CameraStreams = {
	record?: StreamChoice;
	detect?: StreamChoice;
	live?: StreamChoice | 'auto';
	/** go2rtc opens the streams only while something reads them (default: kept open). */
	noPreload?: boolean;
};

export type CameraDetect = { enabled: boolean; objects: string[]; plates: boolean; faces: boolean };
export type CameraRecord = {
	mode: RecordMode;
	retainDays: number;
	preSeconds: number;
	postSeconds: number;
};
export type NotifyRecipient = { user: string; bell: boolean; push: boolean; email: boolean };
export type NotifyWindow = { on: boolean; days: number[]; from: string; to: string };
export type CameraNotify = {
	/** Ways of 0.78; with recipients they only say whether some recipient has the way. */
	titan: boolean;
	push: boolean;
	email: boolean;
	homeAssistant: boolean;
	agent: boolean;
	kinds: string[];
	/** null: a camera of 0.78 (old ways, to the administrators); [] is nobody. */
	recipients?: NotifyRecipient[] | null;
	/** Least seconds between two messages of one kind; absent is 60. */
	pauseSeconds?: number;
	window?: NotifyWindow;
	/** PLAN 83: only while Home Assistant says so; no rules (or null) is no condition. */
	ha?: HACondition | null;
};
/** One entity of Home Assistant and the states in which messages come. */
export type HARule = { connection: string; entity: string; states: string[] };
export type HACondition = { mode: 'all' | 'any'; rules: HARule[] };

/** A camera as administrators get it; other accounts get the first block only. */
export type Camera = {
	id: string;
	name: string;
	enabled: boolean;
	order: number;
	audio: { enabled: boolean; twoWay: boolean; codec?: string; subCodec?: string };
	ptz: boolean;
	state: CameraState;
	/** names: recognised names and plates of this camera (PLAN 82-3). */
	rights?: { live: boolean; recordings: boolean; names?: boolean };
	/**
	 * A removed camera (administrators only): it records nothing and has no
	 * live picture; its recordings and events stay until `expiresAt`.
	 * Times are Unix seconds or ISO text.
	 */
	deleted?: boolean;
	deletedAt?: number | string | null;
	expiresAt?: number | string | null;
	/** A removed camera whose recordings the NAS is deleting right now (PLAN 85-3). */
	purging?: boolean;
	source?: CameraSource;
	streams?: CameraStreams;
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
	/** main and sub: the streams the NAS would take; other: a further ONVIF profile. */
	role: 'main' | 'sub' | 'other';
	url: string;
	/** Name of the ONVIF profile. */
	profile?: string;
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
	decoder?: NvrDecoderStatus;
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
	/** `deleted`: a removed camera (rights do not matter for it any more; left out). */
	/** `names`: recognised faces' names and plates (PLAN 82-3), only with `recordings`. */
	cameras: {
		id: string;
		name: string;
		live: boolean;
		recordings: boolean;
		names?: boolean;
		deleted?: boolean;
	}[];
};
