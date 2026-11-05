import type { IconName } from '#lib/components/Icon.svelte';
// The KI-Agent (PLAN 44): types of the API, the event stream of a running
// chat and small helpers the page and the settings share. No runes here,
// so the helpers can be tested with node --test.

import { apiFetch } from './api.ts';

export type Mode = 'allow' | 'ask' | 'deny';
export type Preset = 'read' | 'standard' | 'all';

export type ToolCall = { function: { name: string; arguments: unknown } };

export type Entry = {
	id: string;
	at: string;
	role: 'user' | 'assistant' | 'tool' | 'note';
	content?: string;
	thinking?: string;
	calls?: ToolCall[];
	tool?: string;
	args?: unknown;
	result?: string;
	ok?: boolean;
	level?: string;
	summary?: string;
	durationMs?: number;
	choices?: string[];
	sources?: { title?: string; url: string }[];
	files?: { root: string; path: string; size?: number }[];
	note?: string;
	stats?: { promptTokens: number; outputTokens: number };
};

export type Pending = {
	id: string;
	tool: string;
	args: unknown;
	level: string;
	summary: string;
	area?: string;
	at: string;
};

export type ChatState = 'idle' | 'running' | 'waiting' | 'interrupted' | 'error';

export type Chat = {
	id: string;
	title: string;
	project?: string;
	pinned?: boolean;
	created: string;
	updated: string;
	model?: string;
	think?: string;
	origin?: string;
	state: ChatState;
	error?: string;
	pending?: Pending | null;
	tainted?: boolean;
	entries: Entry[];
};

export type ChatInfo = Omit<Chat, 'entries' | 'model' | 'think' | 'pending' | 'tainted'> & {
	preview?: string;
	/** Changed since the person last looked, on any device (PLAN 48). */
	unread?: boolean;
	/** Another account wrote it, in a shared project (read-only, PLAN 51). */
	author?: string;
};

export type Project = {
	id: string;
	name: string;
	icon?: string;
	color?: string;
	instructions?: string;
	location?: string;
	created: string;
	/** Accounts that share the project; owner is set for a shared one. */
	members?: string[];
	owner?: string;
};

export type Memory = {
	id: string;
	text: string;
	project?: string;
	chat?: string;
	at: string;
	untrusted?: boolean;
};

export type Skill = {
	id: string;
	name: string;
	when: string;
	steps: string;
	builtIn?: boolean;
	disabled?: boolean;
	untrusted?: boolean;
	updated: string;
};

/** What the morning report contains (PLAN 49), set by each person. */
export type Report = {
	nas?: boolean;
	backups?: boolean;
	today?: boolean;
	home?: boolean;
	extra?: string;
};

export type Schedule = {
	id: string;
	name: string;
	task: string;
	project?: string;
	/** '' a task, 'reminder' only a message, 'morning' the morning report. */
	kind?: '' | 'reminder' | 'morning';
	every:
		| 'once'
		| 'minutes'
		| 'hours'
		| 'days'
		| 'hourly'
		| 'daily'
		| 'weekdays'
		| 'weekly'
		| 'monthly'
		| 'cron'
		| 'trigger';
	at?: string;
	date?: string;
	interval?: number;
	weekdays?: number[];
	cron?: string;
	weekday?: number;
	day?: number;
	report?: Report;
	enabled: boolean;
	lastRun?: string;
	lastOk?: boolean;
	chat?: string;
	trigger?: { entity: string; state: string; for?: number; connection?: string } | null;
	button?: boolean;
	created?: string;
};

export type Model = {
	name: string;
	size: number;
	family?: string;
	parameters?: string;
	capabilities: string[];
	contextLength?: number;
	levels: string[];
	defaultLevel?: string;
};

/** A node of the cluster that asked to use the admin node's model. */
export type AgentShare = { node: string; name: string; allowed: boolean; asked: string };

/** pending/off/unreachable/old/nocluster: why the admin node's model is not usable. */
export type ShareState = 'allowed' | 'pending' | 'off' | 'unreachable' | 'old' | 'nocluster';

/** What the cluster offers on the settings page. */
export type ClusterInfo = {
	member: boolean;
	admin: boolean;
	adminNode?: string;
	state?: ShareState;
};

export type Status = {
	/** The model of the admin node is used, with its state. */
	source?: 'cluster';
	share?: ShareState;
	enabled: boolean;
	allowed: boolean;
	admin: boolean;
	model: string;
	think: string;
	context: number;
	volume: boolean;
	web: boolean;
	reachable?: boolean;
	version?: string;
	error?: string;
	models?: Model[];
	running?: { name: string; contextLength: number; vram: number }[];
	/** Speech input through Whisper is on (PLAN 49). */
	voice?: boolean;
	/** The model that looks at pictures, '' for none. */
	vision?: string;
};

export type Location = { name: string; root: string; path: string; note?: string };
export type FolderRule = { root: string; path: string; mode: Mode };

export type Settings = {
	enabled: boolean;
	ollama?: string;
	/** '' this node's own Ollama, 'cluster' the model of the admin node (PLAN 47). */
	source?: '' | 'cluster';
	/** On the admin node: the nodes that asked for its model. */
	shared?: AgentShare[];
	model?: string;
	think?: string;
	context?: number;
	keepAlive?: string;
	instruction?: string;
	weeklyReport?: boolean;
	maxSteps?: number;
	users?: string[];
	policy: {
		preset: Preset;
		areas?: Record<string, Mode>;
		tools?: Record<string, Mode>;
		folders?: FolderRule[];
	};
	locations?: Location[];
	react?: boolean;
	reactAs?: string;
	skillsOff?: string[];
	homeassistant: {
		/** The agent's own list of released devices (apart from MCP). */
		release?: { domains?: string[]; entities?: string[] };
		// Releases of further Home Assistant connections by their ID.
		releases?: Record<string, { domains?: string[]; entities?: string[] }>;
		entities?: Record<string, Mode>;
		notify?: string[];
		assist?: boolean;
		assistAs?: string;
	};
	/** Speech to text with a Whisper service on the Ollama computer (PLAN 49). */
	voice?: { enabled?: boolean; url?: string; model?: string };
	/** A model of its own for pictures, when the main model sees none. */
	vision?: { enabled?: boolean; model?: string };
	/** Switches off the Btrfs snapshot before changes (PLAN 51). */
	noSnapshots?: boolean;
};

export type ToolView = {
	name: string;
	area: string;
	kind: 'read' | 'create' | 'change';
	dangerous: boolean;
	mode: Mode;
	set: boolean;
};

/** Areas of the settings, in this order (as the backend sends them). */
export const AREAS = [
	'system',
	'files',
	'docker',
	'vms',
	'backups',
	'updates',
	'storage',
	'shares',
	'network',
	'accounts',
	'plugins',
	'home',
	'web',
] as const;

/** Events of a running chat (server-sent events). */
export type AgentEvent =
	| { type: 'thinking'; text: string }
	| { type: 'content'; text: string }
	| { type: 'entry'; entry: Entry }
	| { type: 'step'; step: Entry }
	| { type: 'state'; state: ChatState; error?: string }
	| { type: 'approval'; pending: Pending }
	| { type: 'end' };

/**
 * Follows a running chat until it ends. The stream is fetched (not
 * EventSource) so the cluster node and cookies work as for every request.
 */
export async function follow(
	chatId: string,
	onEvent: (e: AgentEvent) => void,
	signal: AbortSignal,
): Promise<void> {
	const res = await apiFetch(`/agent/chats/${encodeURIComponent(chatId)}/events`, {
		signal,
		headers: { Accept: 'text/event-stream' },
	});
	if (!res.ok || !res.body) {
		onEvent({ type: 'end' });
		return;
	}
	const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
	let buffer = '';
	for (;;) {
		const { value, done } = await reader.read();
		if (done) break;
		buffer += value;
		let cut: number;
		while ((cut = buffer.indexOf('\n\n')) >= 0) {
			const block = buffer.slice(0, cut);
			buffer = buffer.slice(cut + 2);
			const e = parseEvent(block);
			if (e) onEvent(e);
			if (e?.type === 'end') return;
		}
	}
	onEvent({ type: 'end' });
}

/** Parses one block of a server-sent event stream. */
export function parseEvent(block: string): AgentEvent | null {
	let name = '';
	const data: string[] = [];
	for (const line of block.split('\n')) {
		if (line.startsWith(':')) continue;
		if (line.startsWith('event:')) name = line.slice(6).trim();
		else if (line.startsWith('data:')) data.push(line.slice(5).trimStart());
	}
	if (!name) return null;
	if (name === 'end') return { type: 'end' };
	try {
		const parsed = JSON.parse(data.join('\n'));
		return { ...parsed, type: name } as AgentEvent;
	} catch {
		return null;
	}
}

export type Day = 'today' | 'yesterday' | 'week' | 'older';

/** Which group of the chat list a date falls into. */
export function dayGroup(iso: string, now = new Date()): Day {
	const d = new Date(iso);
	const start = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
	const t = d.getTime();
	if (t >= start) return 'today';
	if (t >= start - 86400000) return 'yesterday';
	if (t >= start - 6 * 86400000) return 'week';
	return 'older';
}

/** Groups chats by day, pinned ones first, newest first in each group. */
export function groupChats(
	chats: ChatInfo[],
	now = new Date(),
): { key: Day | 'pinned'; chats: ChatInfo[] }[] {
	const order: (Day | 'pinned')[] = ['pinned', 'today', 'yesterday', 'week', 'older'];
	const groups = new Map<Day | 'pinned', ChatInfo[]>();
	const sorted = [...chats].sort((a, b) => b.updated.localeCompare(a.updated));
	for (const c of sorted) {
		const key = c.pinned ? 'pinned' : dayGroup(c.updated, now);
		groups.set(key, [...(groups.get(key) ?? []), c]);
	}
	return order.filter((k) => groups.has(k)).map((key) => ({ key, chats: groups.get(key)! }));
}

/**
 * The steps of one turn, folded together: everything after a user message
 * up to the answer. Shown as one box "n Schritte".
 */
export type Block =
	| { kind: 'user'; entry: Entry }
	| { kind: 'steps'; steps: Entry[]; thinking: string }
	| { kind: 'answer'; entry: Entry }
	| { kind: 'note'; entry: Entry };

export function blocks(entries: Entry[]): Block[] {
	const out: Block[] = [];
	let steps: Entry[] = [];
	let thinking = '';
	const flush = () => {
		if (steps.length || thinking) out.push({ kind: 'steps', steps, thinking });
		steps = [];
		thinking = '';
	};
	for (const e of entries) {
		if (e.role === 'user') {
			flush();
			out.push({ kind: 'user', entry: e });
		} else if (e.role === 'tool') {
			steps.push(e);
		} else if (e.role === 'assistant') {
			if (e.thinking) thinking += (thinking ? '\n\n' : '') + e.thinking;
			if ((e.content ?? '').trim() || e.choices?.length) {
				flush();
				out.push({ kind: 'answer', entry: e });
			}
		} else if (e.role === 'note') {
			if (e.note === 'approved' || e.note === 'declined') {
				steps.push(e);
				continue;
			}
			flush();
			out.push({ kind: 'note', entry: e });
		}
	}
	flush();
	return out;
}

/** Tools whose steps are written in plain words; others show their name. */
const STEP_WORDS: Record<string, string> = {
	files_list: 'files',
	files_search: 'search',
	file_read: 'read',
	document_read: 'read',
	files_read_text: 'read',
	file_write: 'write',
	files_write_text: 'write',
	files_move: 'move',
	file_copy: 'copy',
	files_copy: 'copy',
	files_mkdir: 'folder',
	files_delete: 'delete',
	docker_list: 'docker',
	container_logs: 'logs',
	logs_search: 'logs',
	vm_list: 'vms',
	storage_overview: 'storage',
	shares_list: 'shares',
	my_shares: 'shares',
	system_status: 'system',
	backup_status: 'backups',
	notifications: 'notifications',
	web_search: 'web',
	web_read: 'page',
	memory_save: 'memory',
	skill_save: 'skill',
	ask_user: 'ask',
	ha_entities: 'home',
	ha_state: 'home',
	ha_call: 'homeCall',
	ha_history: 'home',
	ha_diagnostics: 'home',
};

/** The word key of a tool for agent.step.<word>, or '' to show the name. */
export function stepWord(tool: string | undefined): string {
	return (tool && STEP_WORDS[tool]) || '';
}

/** A short text of the arguments of a step (root and path, query, name). */
export function stepDetail(args: unknown): string {
	if (!args || typeof args !== 'object') return '';
	const a = args as Record<string, unknown>;
	const s = (k: string) => (typeof a[k] === 'string' ? (a[k] as string) : '');
	const where = [s('root'), s('path')]
		.filter(Boolean)
		.join(' › ')
		.replace(/^share\//, '');
	const parts = [where, s('query'), s('url'), s('name'), s('entity_id'), s('vm'), s('container')];
	const text = parts.filter(Boolean).join(' · ');
	return text.length > 90 ? text.slice(0, 89) + '…' : text;
}

/** Whether a step failed (its result starts with "Error:"). */
export function stepFailed(e: Entry): boolean {
	return e.role === 'tool' && !e.ok && (e.result ?? '').startsWith('Error:');
}

/** German/English labels of known thinking levels; unknown ones stay as they are. */
const LEVELS: Record<string, { de: string; en: string }> = {
	off: { de: 'Aus', en: 'Off' },
	on: { de: 'An', en: 'On' },
	minimal: { de: 'Minimal', en: 'Minimal' },
	low: { de: 'Niedrig', en: 'Low' },
	medium: { de: 'Mittel', en: 'Medium' },
	high: { de: 'Hoch', en: 'High' },
	xhigh: { de: 'Sehr hoch', en: 'Extra high' },
	max: { de: 'Maximal', en: 'Max' },
};

export function levelLabel(level: string, locale: 'de' | 'en'): string {
	return LEVELS[level]?.[locale] ?? level;
}

/** The mode of a tool under a policy, like the backend decides (policy.go). */
export function toolMode(
	policy: Settings['policy'],
	tool: { name: string; area: string; kind: string },
): Mode {
	const set = policy.tools?.[tool.name];
	if (set) return set;
	const area = policy.areas?.[tool.area];
	if (area) return area;
	switch (policy.preset) {
		case 'all':
			return 'allow';
		case 'read':
			return tool.kind === 'read' ? 'allow' : 'deny';
	}
	return tool.kind === 'read' || tool.kind === 'create' ? 'allow' : 'ask';
}

/** The mode an area shows: its own, one shared by all its tools, or "mixed". */
export function areaMode(
	policy: Settings['policy'],
	area: string,
	tools: { name: string; area: string; kind: string }[],
): Mode | 'mixed' {
	const own = policy.areas?.[area];
	const modes = new Set(tools.filter((t) => t.area === area).map((t) => toolMode(policy, t)));
	if (own && modes.size <= 1) return own;
	if (modes.size === 1) return [...modes][0];
	return modes.size === 0 ? (own ?? 'allow') : 'mixed';
}

/** Whether the policy is one of the quick choices, untouched. */
export function presetOf(policy: Settings['policy']): Preset | 'custom' {
	const touched =
		Object.keys(policy.areas ?? {}).length > 0 || Object.keys(policy.tools ?? {}).length > 0;
	return touched ? 'custom' : policy.preset;
}

/** Bytes as GB with one decimal (TitanOS shows GB, not GiB). */
export function gb(bytes: number): string {
	return (bytes / 1e9).toFixed(1).replace('.', ',') + ' GB';
}

/** Context length as "32k". */
export function tokens(n: number): string {
	if (!n) return '';
	return n >= 1024 ? `${Math.round(n / 1024)}k` : String(n);
}

/** The look of the agent, kept in this browser only (no stick writes). */
export type Swipe = 'pin' | 'delete' | 'menu' | 'none';
export type Look = {
	accent: 'theme' | 'aurora' | 'ocean' | 'forest' | 'sunset' | 'rose';
	style: 'bubbles' | 'clean' | 'compact';
	background: 'none' | 'aurora' | 'ocean' | 'forest';
	glass: boolean;
	glow: boolean;
	typing: boolean;
	vibrate: boolean;
	calm: boolean;
	/** Read new answers aloud by themselves (PLAN 51); off: a button at each answer. */
	read: boolean;
	// PLAN 53: the agent app, kept per account on the NAS.
	/** How much surfaces show through, 0–100. */
	glassLevel: number;
	fontSize: 's' | 'm' | 'l';
	compact: boolean;
	start: 'new' | 'last' | 'overview';
	suggestions: boolean;
	steps: boolean;
	keepAwake: boolean;
	gestures: boolean;
	edge: boolean;
	pull: boolean;
	swipeLeft: Swipe;
	swipeRight: Swipe;
	motion: 'full' | 'reduced' | 'off';
	rate: number;
	mic: 'tap' | 'hold';
};

/** Settings that belong to the device, not the account. */
export const DEVICE_LOOK: readonly (keyof Look)[] = ['vibrate', 'keepAwake', 'fontSize'];

export const defaultLook = (): Look => ({
	accent: 'aurora',
	style: 'bubbles',
	background: 'aurora',
	glass: true,
	glow: true,
	typing: true,
	vibrate: true,
	calm: false,
	read: false,
	glassLevel: 60,
	fontSize: 'm',
	compact: false,
	start: 'new',
	suggestions: true,
	steps: false,
	keepAwake: true,
	gestures: true,
	edge: true,
	pull: true,
	swipeLeft: 'delete',
	swipeRight: 'pin',
	motion: 'full',
	rate: 1,
	mic: 'tap',
});

export function sanitizeLook(v: unknown): Look {
	const d = defaultLook();
	if (!v || typeof v !== 'object') return d;
	const o = v as Record<string, unknown>;
	const pick = <T extends string>(x: unknown, list: readonly T[], def: T): T =>
		list.includes(x as T) ? (x as T) : def;
	const bool = (x: unknown, def: boolean) => (typeof x === 'boolean' ? x : def);
	const num = (x: unknown, min: number, max: number, def: number) =>
		typeof x === 'number' && Number.isFinite(x) ? Math.min(max, Math.max(min, x)) : def;
	return {
		accent: pick(o.accent, ['theme', 'aurora', 'ocean', 'forest', 'sunset', 'rose'], d.accent),
		style: pick(o.style, ['bubbles', 'clean', 'compact'], d.style),
		background: pick(o.background, ['none', 'aurora', 'ocean', 'forest'], d.background),
		glass: bool(o.glass, d.glass),
		glow: bool(o.glow, d.glow),
		typing: bool(o.typing, d.typing),
		vibrate: bool(o.vibrate, d.vibrate),
		calm: bool(o.calm, d.calm),
		read: bool(o.read, d.read),
		glassLevel: num(o.glassLevel, 0, 100, d.glassLevel),
		fontSize: pick(o.fontSize, ['s', 'm', 'l'], d.fontSize),
		compact: bool(o.compact, d.compact),
		start: pick(o.start, ['new', 'last', 'overview'], d.start),
		suggestions: bool(o.suggestions, d.suggestions),
		steps: bool(o.steps, d.steps),
		keepAwake: bool(o.keepAwake, d.keepAwake),
		gestures: bool(o.gestures, d.gestures),
		edge: bool(o.edge, d.edge),
		pull: bool(o.pull, d.pull),
		swipeLeft: pick(o.swipeLeft, SWIPES, d.swipeLeft),
		swipeRight: pick(o.swipeRight, SWIPES, d.swipeRight),
		motion: pick(o.motion, ['full', 'reduced', 'off'], d.motion),
		rate: num(o.rate, 0.5, 2, d.rate),
		mic: pick(o.mic, ['tap', 'hold'], d.mic),
	};
}

export const SWIPES: readonly Swipe[] = ['pin', 'delete', 'menu', 'none'];

/** The part of a look that is saved for the account on the NAS. */
export function accountLook(look: Look): Partial<Look> {
	return Object.fromEntries(
		Object.entries(look).filter(([k]) => !DEVICE_LOOK.includes(k as keyof Look)),
	) as Partial<Look>;
}

/** The accents of the agent's look: theme accent or a two-color gradient. */
export const ACCENTS: Record<Look['accent'], [string, string] | null> = {
	theme: null,
	aurora: ['#6cb6ff', '#a878ff'],
	ocean: ['#3fb6ff', '#1f6fd6'],
	forest: ['#47d4a0', '#0f9d74'],
	sunset: ['#f2b14a', '#e2457a'],
	rose: ['#ff8fb1', '#c06bff'],
};

/** CSS variables of a look for the root element of the agent. */
/** A suggestion on the start page: a tile that fills the input. */
export type Suggestion = {
	title: string;
	sub: string;
	text: string;
	/** PLAN 53: why it fits now, its symbol and color. */
	tag?: string;
	tone?: 'warn' | 'info' | 'ok';
	icon?: IconName;
	color?: string;
};
/** A warning of the NAS the agent could look into (GET /agent/suggestions). */
export type NasHint = {
	kind: string;
	subject?: string;
	title: string;
	severity: string;
	time: string;
};

export function lookStyle(look: Look): string {
	const pair = ACCENTS[look.accent];
	// PLAN 53: how much the glass shows through and the text size of the app.
	const alpha = (0.32 + (1 - look.glassLevel / 100) * 0.6).toFixed(2);
	const scale = { s: 0.93, m: 1, l: 1.1 }[look.fontSize];
	const base = `--ag-alpha: ${alpha}; --ag-scale: ${scale};`;
	if (!pair) return `${base} --ag-gradient: var(--accent); --ag-accent-2: var(--accent);`;
	return `${base} --accent: ${pair[0]}; --ag-accent-2: ${pair[1]}; --ag-gradient: linear-gradient(135deg, ${pair[0]}, ${pair[1]}); --on-accent: #0b1622;`;
}

// PLAN 53: initiative, memory care and personality of an account (GET/PUT /agent/initiative).
export type Initiative = {
	level: 'quiet' | 'hints' | 'suggest' | 'act';
	reflect: boolean;
	reflectAt: string;
	confirm: boolean;
	morningNote: boolean;
	watch: boolean;
	forecast: boolean;
	events: ('usb' | 'update' | 'crash' | 'ha')[];
	wake: 'wake' | 'ask' | 'never';
	search: { root: string; path: string }[];
	quietFrom: string;
	quietTo: string;
	gpuMinutes: number;
	weekly: boolean;
	memoryAuto: boolean;
	conflicts: boolean;
	fadeMonths: number;
	learnSkills: boolean;
	length: 'short' | 'normal' | 'long';
	tone: 'casual' | 'plain';
	name: string;
	icon: string;
};

export const defaultInitiative = (): Initiative => ({
	level: 'suggest',
	reflect: true,
	reflectAt: '03:30',
	confirm: true,
	morningNote: true,
	watch: true,
	forecast: true,
	events: ['usb', 'update', 'crash', 'ha'],
	wake: 'wake',
	search: [],
	quietFrom: '22:00',
	quietTo: '07:00',
	gpuMinutes: 30,
	weekly: true,
	memoryAuto: true,
	conflicts: true,
	fadeMonths: 6,
	learnSkills: true,
	length: 'normal',
	tone: 'casual',
	name: '',
	icon: '',
});

export function sanitizeInitiative(v: unknown): Initiative {
	const d = defaultInitiative();
	if (!v || typeof v !== 'object') return d;
	const o = v as Record<string, unknown>;
	const pick = <T extends string>(x: unknown, list: readonly T[], def: T): T =>
		list.includes(x as T) ? (x as T) : def;
	const bool = (x: unknown, def: boolean) => (typeof x === 'boolean' ? x : def);
	const clock = (x: unknown, def: string) =>
		typeof x === 'string' && /^([01]\d|2[0-3]):[0-5]\d$/.test(x) ? x : def;
	const num = (x: unknown, lo: number, hi: number, def: number) =>
		typeof x === 'number' && Number.isFinite(x) ? Math.min(hi, Math.max(lo, Math.round(x))) : def;
	const events = Array.isArray(o.events)
		? (o.events.filter((e) => ['usb', 'update', 'crash', 'ha'].includes(e)) as Initiative['events'])
		: d.events;
	const search = Array.isArray(o.search)
		? o.search
				.filter(
					(s) => s && typeof s === 'object' && typeof (s as { root?: unknown }).root === 'string',
				)
				.map((s) => ({
					root: (s as { root: string }).root,
					path: String((s as { path?: unknown }).path ?? ''),
				}))
		: [];
	return {
		level: pick(o.level, ['quiet', 'hints', 'suggest', 'act'], d.level),
		reflect: bool(o.reflect, d.reflect),
		reflectAt: clock(o.reflectAt, d.reflectAt),
		confirm: bool(o.confirm, d.confirm),
		morningNote: bool(o.morningNote, d.morningNote),
		watch: bool(o.watch, d.watch),
		forecast: bool(o.forecast, d.forecast),
		events,
		wake: pick(o.wake, ['wake', 'ask', 'never'], d.wake),
		search,
		quietFrom: clock(o.quietFrom, d.quietFrom),
		quietTo: clock(o.quietTo, d.quietTo),
		gpuMinutes: num(o.gpuMinutes, 5, 120, d.gpuMinutes),
		weekly: bool(o.weekly, d.weekly),
		memoryAuto: bool(o.memoryAuto, d.memoryAuto),
		conflicts: bool(o.conflicts, d.conflicts),
		fadeMonths: num(o.fadeMonths, 0, 24, d.fadeMonths),
		learnSkills: bool(o.learnSkills, d.learnSkills),
		length: pick(o.length, ['short', 'normal', 'long'], d.length),
		tone: pick(o.tone, ['casual', 'plain'], d.tone),
		name: typeof o.name === 'string' ? o.name.slice(0, 24) : '',
		icon: typeof o.icon === 'string' ? [...o.icon].slice(0, 4).join('') : '',
	};
}

/** A suggestion of the agent waiting for a yes or no (GET /agent/inbox). */
export type InboxItem = {
	id: string;
	kind: 'memory' | 'conflict' | 'fade' | 'skill' | 'forecast' | 'alert';
	title: string;
	body?: string;
	data?: Record<string, string>;
	created: string;
};
